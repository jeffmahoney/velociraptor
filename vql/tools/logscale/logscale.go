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
	"sync/atomic"
	"time"

	"github.com/Velocidex/ordereddict"
	"golang.org/x/sync/errgroup"
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

	listener                  *directory.Listener
	workerGrp                 *errgroup.Group
	workerCtx		  context.Context

	httpClient                *http.Client
	httpTransport             *http.Transport
	// (bool) whether the queue is open
	queueOpened		  int64

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

	// Statistics
	// count of events queued for posting across all workers
	currentQueueDepth	  int64
	// count of events dropped during shutdown across all workers
	droppedEvents		  int64
	// count of events successfully posted
	postedEvents		  int64
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
	return self.nWorkers
}

func (self *LogScaleQueue) SetWorkerCount(count int) error {
	if self.Opened() {
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
	if self.Opened() {
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
	if self.Opened() {
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
	if self.Opened() {
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
	if self.Opened() {
		return errQueueOpened
	}

	self.maxRetries = max
	return nil
}

func (self *LogScaleQueue) SetTaggedFields(tags []string) error {
	if self.Opened() {
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
	if self.Opened() {
		return errQueueOpened
	}

	self.httpTransport = transport
	return nil
}

func (self *LogScaleQueue) Open(scope vfilter.Scope, baseUrl string, authToken string) error {
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

	// We want to do our own cleanup, which has a pipeline we want to flush if we can
	self.workerGrp, self.workerCtx = errgroup.WithContext(context.Background())

	self.httpClient = &http.Client{Timeout: self.httpClientTimeoutDuration,
				       Transport: transport}

	self.id = int(atomic.AddInt64(&gNextId, 1))
	self.logPrefix = fmt.Sprintf("logscale/%v: ", self.id)

	options := api.QueueOptions{
		DisableFileBuffering: false,
		FileBufferLeaseSize: 100,
		OwnerName: "logscale-plugin",
	}
	self.listener, err = directory.NewListener(self.config, self.workerCtx,
						   options.OwnerName, options)
	if err != nil {
		self.workerGrp.Wait()
		return err
	}

	self.queueOpened = 1
	for i := 0; i < self.nWorkers; i++ {
		self.workerGrp.Go(func() error {
			return self.processEvents(self.workerCtx, scope)
		})
	}

	return nil
}

func (self *LogScaleQueue) Opened() bool {
	return atomic.LoadInt64(&self.queueOpened) > 0
}

func (self *LogScaleQueue) addDebugCallback(count int, callback func(int)) error {
	if self.Opened() {
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

func (self *LogScaleQueue) postBytes(ctx context.Context, scope vfilter.Scope,
				  data []byte, count int) (error, bool) {
	req, err := http.NewRequestWithContext(ctx, "POST", self.endpointUrl,
					       bytes.NewReader(data))
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
	_, err = utils.Copy(ctx, body, resp.Body)
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
		err, retry := self.postBytes(ctx, scope, data, nRows)
		if err == nil || err == io.EOF {
			if failed {
				self.Log(scope, "Retry successful, pushing backlog.")
			}
			if err == nil {
				atomic.AddInt64(&self.postedEvents, int64(nRows))
			}
			return err
		}

		failed = true
		wait := time.Duration(gMaxPoll + rand.Intn(gMaxPollDev)) * time.Second

		// If the queue is shutting down and we're failing to post messages, stop
		// trying and fail fast so we can exit.
		if atomic.LoadInt64(&self.queueOpened) == 0 {
			self.Log(scope, "Failed to POST events, will not retry due to plugin shutting down. Dropped %v events.", nRows)
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
			self.Log(scope, "adding to retries")
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

func (self *LogScaleQueue) processEvents(ctx context.Context, scope vfilter.Scope) error {
	postData := []*ordereddict.Dict{}
	eventCount := 0
	dropEvents := false
	totalEventCount := 0

	defer self.Debug(scope, "worker exited")
	defer func() { self.postEvents(ctx, scope, postData) }()

	self.Debug(scope, "worker started")

	clock := utils.GetTime()

	for {
		postEvents := false

		// We don't watch the context because the context isn't meant to be canceled
		select {
		case <-clock.After(self.batchingTimeoutDuration):
			if eventCount > 0 {
				postEvents = true
			}
		case row, ok := <-self.listener.Output():
			if !ok {
				self.Debug(scope, "worker exiting due to closed channel")
				return nil
			}

			if self.debugEventsEnabled {
				self.debugEvents(totalEventCount)
			}

			self.Debug(scope, "dequeued event/1")
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

func (self *LogScaleQueue) Close(scope vfilter.Scope) {
	if !atomic.CompareAndSwapInt64(&self.queueOpened, 1, 0) {
		// Already shut down
		return
	}
	backlog := atomic.LoadInt64(&self.currentQueueDepth)
	if backlog > 0 {
		self.Log(scope, "Plugin shutting down.  There is a backlog of %v events that will be processed in the background before the plugin finally exits.  If this artifact was reconfigured, another invocation will proceed normally in parallel.", backlog)
	}

	if self.listener != nil {
		self.listener.Close()
	}

	var err error
	if self.workerGrp != nil {
		err = self.workerGrp.Wait()
	}

	dropped := atomic.LoadInt64(&self.droppedEvents)
	backlog = atomic.LoadInt64(&self.currentQueueDepth)
	if err != nil || (dropped + backlog) > 0 {
		self.Log(scope, "Queue closed with %v dropped events: %v", dropped + backlog, err)
	}
}

func (self *LogScaleQueue) Log(scope vfilter.Scope, fmt string, args ...any) {
	scope.Log(self.logPrefix + fmt, args...)
}

func (self *LogScaleQueue) Debug(scope vfilter.Scope, fmt string, args ...any) {
	if self.debug {
		scope.Log(self.logPrefix + fmt, args...)
	}
}

func (self *LogScaleQueue) PostBacklogStats(scope vfilter.Scope) {
	currentQueueDepth := atomic.LoadInt64(&self.currentQueueDepth)
	queuedBytes := self.listener.FileBufferSize()
	self.Log(scope, "Backlog size: %v events %v bytes", currentQueueDepth, queuedBytes)
}

type logscalePlugin struct{}

func (self *LogScaleQueue) EnableDebugging(enabled bool) {
	self.debug = enabled
}

