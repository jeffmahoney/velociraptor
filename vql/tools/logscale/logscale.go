package logscale

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/Velocidex/ordereddict"
	"www.velocidex.com/golang/velociraptor/constants"
	config_proto "www.velocidex.com/golang/velociraptor/config/proto"
	"www.velocidex.com/golang/velociraptor/file_store/api"
	"www.velocidex.com/golang/velociraptor/file_store/directory"
	"www.velocidex.com/golang/velociraptor/json"
	"www.velocidex.com/golang/velociraptor/services"
	"www.velocidex.com/golang/velociraptor/utils"
	vql_subsystem "www.velocidex.com/golang/velociraptor/vql"
	"www.velocidex.com/golang/velociraptor/vql/functions"
	"www.velocidex.com/golang/velociraptor/vql/networking"
	vfilter "www.velocidex.com/golang/vfilter"
)

var (
	defaultEventBatchSize = 2000
	defaultBatchingTimeoutDuration = time.Duration(3000) * time.Millisecond
	defaultHttpClientTimeoutDuration = time.Duration(10) * time.Second
	defaultNWorkers = 1
	defaultMaxRetries = 7200 // ~2h more or less

	gMaxPoll = 60
	gMaxPollDev = 30
	gStatsLogPeriod = time.Duration(30) * time.Second
	gNextId int64 = 0

	apiEndpoint = "/v1/ingest/humio-structured"
)

type errInvalidArgument struct {
	Arg string
	Err error
}

func (err errInvalidArgument) Error() string {
	return fmt.Sprintf("Invalid Argument - %s", err.Err)
}

func (err errInvalidArgument) Is(other error) bool {
	return errors.Is(err.Err, other)
}

type errHttpClientError struct {
	StatusCode int
	Status string
}

func (err errHttpClientError) Error() string {
	return err.Status
}

type errMaxRetriesExceeded struct {
	LastError error
	Retries int
}

func (err errMaxRetriesExceeded) Error() string {
	return fmt.Sprintf("Maximum retries exceeded: %v attempts, last error=%s",
			   err.Retries, err.LastError)
}

func (err errMaxRetriesExceeded) Unwrap() error {
	return err.LastError
}

var errQueueOpened = errors.New("Cannot modify parameters of open queue")

type LogScaleQueue struct {
	scope                  	  vfilter.Scope
	config                 	  *config_proto.Config
	lock			  sync.Mutex
	cancel			  func()

	listener                  *directory.Listener
	workerWg		  sync.WaitGroup

	httpClient                *http.Client
	httpTransport             *http.Transport
	opened			  bool

	endpointUrl               string
	authToken		  string
	nWorkers                  int
	tagMap                    map[string]string
	batchingTimeoutDuration   time.Duration
	httpClientTimeoutDuration time.Duration
	eventBatchSize            int
	httpTimeout		  int
	debug			  bool
	debugEventsEnabled	  bool
	debugEventsMap		  map[int][]func(int)
	id			  int
	logPrefix		  string
	maxRetries		  int
	queueClosing		  int64

	// Statistics
	// count of events queued for posting across all workers
	currentQueueDepth	  int64
	// count of events dropped during shutdown across all workers
	droppedEvents		  int64
	// count of events successfully posted
	postedEvents		  int64
	// count of bytes successfully posted
	postedBytes		  int64
	// count of events that failed to post
	failedEvents		  int64
	// count of retries since startup
	totalRetries		  int64
}

type LogScaleEvent struct {
	Timestamp time.Time          `json:"timestamp"`
	Attributes *ordereddict.Dict `json:"attributes"`
	Timezone string              `json:"timezone,omitempty"`
}

type LogScalePayload struct {
	Events []LogScaleEvent          `json:"events"`
	Tags map[string]interface{}  `json:"tags,omitempty"`
}

func (self *LogScalePayload) String() string {
	data, err := json.Marshal(self)
	if err != nil {
		return fmt.Sprintf("LogScalePayload{unprintable, err=%s}", err)
	}

	return string(data)
}

func NewLogScaleQueue(config_obj *config_proto.Config) *LogScaleQueue {
	return &LogScaleQueue{
		config: config_obj,
		nWorkers: defaultNWorkers,
		batchingTimeoutDuration: defaultBatchingTimeoutDuration,
		eventBatchSize: defaultEventBatchSize,
		httpClientTimeoutDuration: defaultHttpClientTimeoutDuration,
		maxRetries: defaultMaxRetries,
	}
}

func (self *LogScaleQueue) WorkerCount() int {
	self.lock.Lock()
	defer self.lock.Unlock()

	return self.nWorkers
}

func (self *LogScaleQueue) SetWorkerCount(count int) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	if self.opened {
		return errQueueOpened
	}
	if count <= 0 {
		return errInvalidArgument{
			Arg: "count",
			Err: fmt.Errorf("value must be positive integer"),
		}
	}

	self.nWorkers = count
	return nil
}

func (self *LogScaleQueue) SetBatchingTimeoutDuration(timeout time.Duration) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	if self.opened {
		return errQueueOpened
	}
	if timeout <= 0 {
		return errInvalidArgument{
			Arg: "timeout",
			Err: fmt.Errorf("value must be positive integer"),
		}
	}

	self.batchingTimeoutDuration = timeout
	return nil
}

func (self *LogScaleQueue) SetEventBatchSize(size int) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	if self.opened {
		return errQueueOpened
	}
	if size <= 0 {
		return errInvalidArgument{
			Arg: "size",
			Err: fmt.Errorf("value must be positive integer"),
		}
	}

	self.eventBatchSize = size
	return nil
}

func (self *LogScaleQueue) SetHttpClientTimeoutDuration(timeout time.Duration) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	if self.opened {
		return errQueueOpened
	}
	if timeout <= 0 {
		return errInvalidArgument{
			Arg: "timeout",
			Err: fmt.Errorf("value must be positive integer"),
		}
	}

	self.httpClientTimeoutDuration = timeout
	return nil
}

func (self *LogScaleQueue) SetMaxRetries(max int) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	if self.opened {
		return errQueueOpened
	}

	self.maxRetries = max
	return nil
}

func (self *LogScaleQueue) SetTaggedFields(tags []string) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	if self.opened {
		return errQueueOpened
	}

	var tagMapping map[string]string
	if len(tags) > 0 {
		tagMapping = map[string]string{}

		for _, descr := range tags {
			colName, tagName, mapped := strings.Cut(descr, "=")

			if colName == "" {
				return errInvalidArgument{
					Arg: "tags",
					Err: fmt.Errorf("Empty column name is not valid"),
				}
			}

			if mapped {
				if tagName == "" {
					return errInvalidArgument{
						Arg: "tags",
						Err: fmt.Errorf("Empty tag name is not valid"),
					}
				}
			} else {
				tagName = colName
			}

			tagMapping[colName] = tagName
		}
	}
	self.tagMap = tagMapping

	return nil
}

func (self *LogScaleQueue) SetHttpTransport(transport *http.Transport) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	if self.opened {
		return errQueueOpened
	}

	self.httpTransport = transport
	return nil
}

func (self *LogScaleQueue) Open(parentCtx context.Context, scope vfilter.Scope,
				baseUrl string, authToken string) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	self.endpointUrl = baseUrl + apiEndpoint
	self.authToken = authToken

	if baseUrl == "" {
		return errInvalidArgument{
			Arg: "baseUrl",
			Err: errors.New("invalid value - must not be empty"),
		}
	}

	_, err := url.ParseRequestURI(self.endpointUrl)
	if err != nil {
		return errInvalidArgument{
			Arg: "baseUrl",
			Err: err,
		}
	}

	if authToken == "" {
		return errInvalidArgument{
			Arg: "authToken",
			Err: errors.New("invalid value - must not be empty"),
		}
	}

	transport := self.httpTransport
	if transport == nil {
		transport, err = networking.GetHttpTransport(self.config.Client, "")
		if err != nil {
			return err
		}
	}

	self.httpClient = &http.Client{Timeout: self.httpClientTimeoutDuration,
				       Transport: transport}

	self.id = int(atomic.AddInt64(&gNextId, 1))
	self.logPrefix = fmt.Sprintf("logscale/%v: ", self.id)

	options := api.QueueOptions{
		DisableFileBuffering: false,
		FileBufferLeaseSize: 100,
		OwnerName: "logscale-plugin",
	}

	// Canceling the listener's context prevents it from clearing its queue
	ctx, cancel := context.WithCancel(context.Background())
	self.listener, err = directory.NewListener(self.config, ctx, options.OwnerName, options)
	if err != nil {
		return err
	}

	err = vql_subsystem.GetRootScope(scope).AddDestructor(func() {
		cancel()
		if self.listener != nil {
			self.listener.Close()
		}
	})


	ctx, self.cancel = context.WithCancel(parentCtx)
	self.opened = true
	for i := 0; i < self.nWorkers; i++ {
		self.workerWg.Add(1)
		go self.processEvents(ctx, scope)
	}

	return nil
}

func (self *LogScaleQueue) addDebugCallback(count int, callback func(int)) error {
	self.lock.Lock()
	defer self.lock.Unlock()

	if self.opened {
		return errQueueOpened
	}

	if !self.debugEventsEnabled {
		self.debugEventsMap = map[int][]func(int){}
		self.debugEventsEnabled = true
	}

	_, ok := self.debugEventsMap[count]
	if ok {
		self.debugEventsMap[count] = append(self.debugEventsMap[count], callback)
	} else {
		self.debugEventsMap[count] = []func(int){callback}
	}

	return nil
}

// Provide the hostname for the client host if it's a client query
// since an external system will not have a way to map it to a hostname.
func (self *LogScaleQueue) addClientInfo(ctx context.Context, row *ordereddict.Dict,
				      payload *LogScalePayload) {
	client_id, ok := row.GetString("ClientId")
	if ok {
		payload.Tags["ClientId"] = client_id

		hostname := services.GetHostname(ctx, self.config, client_id)
		if hostname != "" {
			payload.Tags["ClientHostname"] = hostname
		}
	}
}

func (self *LogScaleQueue) addMappedTags(row *ordereddict.Dict, payload *LogScalePayload) {
	for name, mappedName := range self.tagMap {
		value, ok := row.Get(name)

		if ok {
			payload.Tags[mappedName] = value
		}
	}
}

func (self *LogScaleQueue) addTimestamp(scope vfilter.Scope, row *ordereddict.Dict,
				     payload *LogScalePayload) {
	timestamp, ok := row.Get("Time")
	if !ok {
		timestamp, ok = row.Get("timestamp")
	}
	if !ok {
		timestamp, ok = row.Get("_ts")
	}
	var ts time.Time
	if ok {
		// It's only an error if it's nil, and it can't be.
		ts, _ = functions.TimeFromAny(scope, timestamp)
	} else {
		ts = time.Now()
	}

	payload.Events[0].Timestamp = ts
}

func NewLogScalePayload(row *ordereddict.Dict) *LogScalePayload {
	return  &LogScalePayload{
		Events: []LogScaleEvent {
			LogScaleEvent{
				Attributes: row,
			},
		},
		Tags: map[string]interface{}{},
	}
}

func (self *LogScaleQueue) rowToPayload(ctx context.Context, scope vfilter.Scope,
				     row *ordereddict.Dict) *LogScalePayload {
	payload := NewLogScalePayload(row)

	self.addClientInfo(ctx, row, payload)
	self.addMappedTags(row, payload)
	self.addTimestamp(scope, row, payload)

	return payload
}

func (self *LogScaleQueue) postBytes(scope vfilter.Scope, data []byte, count int) (error, bool) {
	req, err := http.NewRequest("POST", self.endpointUrl, bytes.NewReader(data))
	req.Header.Add("User-Agent", constants.USER_AGENT)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", self.authToken))

	resp, err := self.httpClient.Do(req)
	if err != nil && err != io.EOF {
		self.Log(scope, "request failed: %s", err)
		return err, true
	}
	defer resp.Body.Close()

	self.Debug(scope, "sent %d events %d bytes, response with status: %v",
		   count, len(data), resp.Status)
	body := &bytes.Buffer{}
	_, err = io.Copy(body, resp.Body)
	if err != nil {
		self.Log(scope, "copy of response failed: %v, %v", resp.Status, err)
		return err, false
	}

	if resp.StatusCode != 200 {
		self.Log(scope, "request failed: %v, %v", resp.Status, body)
		retry := true
		// In general these will require restarting the artifact, which will
		// lose the events anyway.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			retry = false
		}
		return errHttpClientError{
			StatusCode: resp.StatusCode,
			Status: resp.Status,
			}, retry
	}

	return nil, false
}

var errQueueShutdown = errors.New("Queue shutdown while server was not accepting events.")

func (self *LogScaleQueue) postEvents(ctx context.Context, scope vfilter.Scope,
				   rows []*ordereddict.Dict) error {
	nRows := len(rows)
	opts := vql_subsystem.EncOptsFromScope(scope)

	self.Debug(scope, "posting %v events", nRows)

	// Can happen during plugin shutdown
	if nRows == 0 {
		return nil
	}

	clock := utils.GetTime()

	payloads := []*LogScalePayload{}
	for _, row := range(rows) {
		payloads = append(payloads, self.rowToPayload(ctx, scope, row))
	}

	data, err := json.MarshalWithOptions(payloads, opts)
	if err != nil {
		return fmt.Errorf("Failed to encode %v: %w", rows, err)
	}

	failed := false
	retries := 0
	for {
		err, retry := self.postBytes(scope, data, nRows)
		if err == nil || err == io.EOF {
			if failed {
				self.Log(scope, "Retry successful, pushing backlog.")
			}
			if err == nil {
				atomic.AddInt64(&self.postedEvents, int64(nRows))
				atomic.AddInt64(&self.postedBytes, int64(len(data)))
			}
			return err
		}

		failed = true
		wait := time.Duration(gMaxPoll + rand.Intn(gMaxPollDev)) * time.Second

		if ctx.Err() != nil {
			self.Log(scope, "Failed to POST events while queue is closing.  Dropping remaining events.")
			atomic.AddInt64(&self.failedEvents, int64(nRows))
			return errQueueShutdown
		} else if retry {
			retries += 1
			if self.maxRetries >= 0 && retries > self.maxRetries {
				atomic.AddInt64(&self.failedEvents, int64(nRows))
				return errMaxRetriesExceeded{
					LastError: err,
					Retries: retries,
				}
			}
			self.Log(scope, "Failed to POST events, will attempt retry #%v in %v.",
				 retries, wait)
		} else {
			// We want to wait after 4xx errors or we'll just spam the server
			// if something is wrong.
			self.Log(scope, "Failed to POST events, will not retry due to client error. Dropped %v events. Waiting %v before attempting next submission.", nRows, wait)
			atomic.AddInt64(&self.failedEvents, int64(nRows))
		}

		select {
		case <- ctx.Done():
			return errQueueShutdown
		case <-clock.After(wait):
		}

		if retry {
			atomic.AddInt64(&self.totalRetries, 1)
		} else {
			return err
		}
	}
}

func (self *LogScaleQueue) debugEvents(count int) {

	events, ok := self.debugEventsMap[count]
	if ok  {
		for _, callback := range events {
			callback(count)
		}
	}
}

func (self *LogScaleQueue) processEvents(ctx context.Context, scope vfilter.Scope) {
	postData := []*ordereddict.Dict{}
	eventCount := 0
	dropEvents := false
	totalEventCount := 0

	defer self.workerWg.Done()
	defer self.Debug(scope, "worker exited")
	defer func() { self.postEvents(ctx, scope, postData) }()

	self.Debug(scope, "worker started")

	clock := utils.GetTime()

	for {
		postEvents := false

		// We don't watch the context because we need to clear the queue first.
		// The context cancelation will close the listener, which will close
		// the output channel once the queue is flushed.
		select {
		case <-clock.After(self.batchingTimeoutDuration):
			if eventCount > 0 {
				postEvents = true
			}
		case row, ok := <-self.listener.Output():
			if !ok {
				self.Debug(scope, "worker exiting due to closed channel")
				return
			}

			if self.debugEventsEnabled {
				self.debugEvents(totalEventCount)
			}

			atomic.AddInt64(&self.currentQueueDepth, -1)

			if dropEvents {
				atomic.AddInt64(&self.droppedEvents, 1)
				continue
			}

			postData = append(postData, row)
			eventCount += 1
			totalEventCount += 1

			self.Debug(scope, "dequeued event/2 %v %v", totalEventCount, len(postData))
			if eventCount >= self.eventBatchSize {
				postEvents = true
			}
		}

		if postEvents {
			// This will block until all the events are posted, even (and especially)
			// if the server is down or the network is disrupted.  This is why
			// we use a ring buffer to queue.  There are cases in which a failure
			// is permanent. Those will be logged and events dropped.
			err := self.postEvents(ctx, scope, postData)
			if err != nil {
				self.Log(scope, "Failed to post events, lost events: %v", err)

				if errors.Is(err, errQueueShutdown) {
					self.Debug(scope, "worker exiting due to queue shutdown")
					dropEvents = true
				}
			}

			postData = []*ordereddict.Dict{}
			eventCount = 0
		}
	}
}

func (self *LogScaleQueue) QueueEvent(row *ordereddict.Dict) {
	self.listener.Send(row)
	atomic.AddInt64(&self.currentQueueDepth, 1)
}

// This can be racy but it's only used to determine whether we should skip retries
func (self *LogScaleQueue) Closing() bool {
	return atomic.LoadInt64(&self.queueClosing) != 0
}

func (self *LogScaleQueue) Close(scope vfilter.Scope) {
	self.lock.Lock()
	defer self.lock.Unlock()

	if !self.opened {
		return
	}

	self.opened = false
	atomic.StoreInt64(&self.queueClosing, 1)

	backlog := atomic.LoadInt64(&self.currentQueueDepth)
	if backlog > 0 {
		self.Log(scope, "Closing submission queue. There is a backlog of %v events that will be processed prior to completion.", backlog)
	}

	// Order is important:
	// Closing the listener after canceling it will drop all remaining events, so
	// we can't cancel it.
	// If we close the listener before canceling the workers, the workers will
	// continue to retry posting events after failure and closing could take a
	// very long time.
	self.cancel()

	// Stop listening for more events
	self.listener.Close()

	// Wait for workers to finish flushing
	self.workerWg.Wait()

	// The listener needs to be valid until all workers exit
	self.listener = nil

	dropped := atomic.LoadInt64(&self.droppedEvents)
	backlog = atomic.LoadInt64(&self.currentQueueDepth)
	if (dropped + backlog) > 0 {
		self.Log(scope, "Queue closed with %v dropped events", dropped + backlog)
	}
	atomic.StoreInt64(&self.queueClosing, 0)
}

func (self *LogScaleQueue) Log(scope vfilter.Scope, fmt string, args ...any) {
	scope.Log(self.logPrefix + fmt, args...)
}

func (self *LogScaleQueue) Debug(scope vfilter.Scope, fmt string, args ...any) {
	if self.debug {
		scope.Log(self.logPrefix + fmt, args...)
	}
}

func (self *LogScaleQueue) PostStats(scope vfilter.Scope) {
	currentQueueDepth := atomic.LoadInt64(&self.currentQueueDepth)
	queuedBytes := self.listener.FileBufferSize()
	postedEvents := atomic.LoadInt64(&self.postedEvents)
	postedBytes := atomic.LoadInt64(&self.postedBytes)
	droppedEvents := atomic.LoadInt64(&self.failedEvents)
	totalRetries := atomic.LoadInt64(&self.totalRetries)

	self.Log(scope, "Posted %v events %v bytes, backlog of %v events %v bytes, %v events dropped, %v retries",
		 postedEvents, postedBytes, currentQueueDepth, queuedBytes, droppedEvents,
		 totalRetries)
}

type logscalePlugin struct{}

func (self *LogScaleQueue) EnableDebugging(enabled bool) {
	self.debug = enabled
}

