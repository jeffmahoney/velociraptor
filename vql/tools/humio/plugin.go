package humio

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/Velocidex/ordereddict"
	"www.velocidex.com/golang/velociraptor/acls"
	vql_subsystem "www.velocidex.com/golang/velociraptor/vql"
	"www.velocidex.com/golang/velociraptor/vql/networking"
	vfilter "www.velocidex.com/golang/vfilter"
	"www.velocidex.com/golang/vfilter/arg_parser"
)

type humioPluginArgs struct {
	Query              vfilter.StoredQuery `vfilter:"required,field=query,doc=Source for rows to upload."`
	ApiBaseUrl	   string   `vfilter:"required,field=apibaseurl,doc=Base URL for Ingestion API"`
	IngestToken	   string   `vfilter:"required,field=ingest_token,doc=Ingest token for API"`
	Threads            int      `vfilter:"optional,field=threads,doc=How many threads to use to post batched events."`
	HttpTimeoutSec	   int	    `vfilter:"optional,field=http_timeout,doc=Timeout for http requests (default: 10s)"`
	MaxRetries	   int      `vfilter:"optional,field=max_retries,doc=Maximum number of retries before failing an operation. A value < 0 means retry forever. (default: 7200)"`
	RootCerts          string   `vfilter:"optional,field=root_ca,doc=As a better alternative to skip_verify, allows root ca certs to be added here."`
	SkipVerify	   bool	    `vfilter:"optional,field=skip_verify,doc=Skip verification of server certificates (default: false)"`
	BatchingTimeoutMs  int      `vfilter:"optional,field=batching_timeout_ms,doc=Timeout between posts (default: 3000ms)"`
	EventBatchSize	   int	    `vfilter:"optional,field=event_batch_size,doc=Items to batch before post (default: 2000)"`
	TagFields	   []string `vfilter:"optional,field=tag_fields,doc=Name of fields to be used as tags. Fields can be renamed using =<newname>"`
	Debug		   bool     `vfilter:"optional,field=debug,doc=Enable verbose logging."`
}

func (args *humioPluginArgs) validate() error {
	if args.ApiBaseUrl == "" {
		return errInvalidArgument{
			Arg: "apibaseurl",
			Err: errors.New("invalid value - must not be empty"),
		}
	}

	_, err := url.ParseRequestURI(args.ApiBaseUrl)
	if err != nil {
		return errInvalidArgument{
			Arg: "apibaseurl",
			Err: err,
		}
	}

	if args.IngestToken == "" {
		return errInvalidArgument{
			Arg: "ingest_token",
			Err: errors.New("invalid value - must not be empty"),
		}
	}

	if args.Threads < 0 {
		return errInvalidArgument{
			Arg: "threads",
			Err: fmt.Errorf("invalid value %v - must be 0 or positive integer",
					args.Threads),
		}
	}

	if args.BatchingTimeoutMs < 0 {
		return errInvalidArgument{
			Arg: "batching_timeout_ms",
			Err: fmt.Errorf("invalid value %v - must be 0 or positive integer",
					args.BatchingTimeoutMs),
		}
	}

	if args.EventBatchSize < 0 {
		return errInvalidArgument{
			Arg: "event_batch_size",
			Err: fmt.Errorf("invalid value %v - must be 0 or positive integer",
					args.EventBatchSize),
		}
	}

	if args.HttpTimeoutSec < 0 {
		return errInvalidArgument{
			Arg: "http_timeout",
			Err: fmt.Errorf("invalid value %v - must be 0 or positive integer",
					args.HttpTimeoutSec),
		}
	}

	return nil
}

func applyArgs(args *humioPluginArgs, queue *HumioQueue) error {
	var err error
	if args.Threads > 0 {
		err = queue.SetWorkerCount(args.Threads)
		if err != nil {
			return fmt.Errorf("`threads': %w", err)
		}
	}

	if args.BatchingTimeoutMs > 0 {
		timeout := time.Duration(args.BatchingTimeoutMs) * time.Millisecond
		err = queue.SetBatchingTimeoutDuration(timeout)
		if err != nil {
			return fmt.Errorf("`batching_timeout_ms': %w", err)
		}
	}

	if args.EventBatchSize > 0 {
		err = queue.SetEventBatchSize(args.EventBatchSize)
		if err != nil {
			return fmt.Errorf("`event_batch_size': %w", err)
		}
	}

	if args.HttpTimeoutSec > 0 {
		timeout := time.Duration(args.HttpTimeoutSec) * time.Second
		err = queue.SetHttpClientTimeoutDuration(timeout)
		if err != nil {
			return fmt.Errorf("`http_timeout': %w", err)
		}
	}

	if args.MaxRetries != defaultMaxRetries {
		err = queue.SetMaxRetries(args.MaxRetries)
		if err != nil {
			return fmt.Errorf("`max_retries': %s", err)
		}
	}

	err = queue.SetTaggedFields(args.TagFields)
	if err != nil {
		return fmt.Errorf("`tag_fields': %w", err)
	}

	if args.Debug {
		queue.EnableDebugging(true)
	}

	return nil
}

func (self humioPlugin) Call(ctx context.Context,
	scope vfilter.Scope,
	args *ordereddict.Dict) <-chan vfilter.Row {
	outputChan := make(chan vfilter.Row)

	go func() {
		defer close(outputChan)

		err := vql_subsystem.CheckAccess(scope, acls.COLLECT_SERVER)
		if err != nil {
			scope.Log("humio: %v", err)
			return
		}

		arg := humioPluginArgs{
			MaxRetries: defaultMaxRetries,
		}
		err = arg_parser.ExtractArgsWithContext(ctx, scope, args, &arg)
		if err != nil {
			scope.Log("humio: %v", err)
			return
		}

		err = arg.validate()
		if err != nil {
			scope.Log("humio: %v", err)
			return
		}

		config_obj, ok := vql_subsystem.GetServerConfig(scope)
		if !ok {
			scope.Log("humio: could not get config from scope")
			return
		}

		queue := NewHumioQueue(config_obj)

		err = applyArgs(&arg, queue)
		if err != nil {
			scope.Log("humio: %v", err)
			return
		}

		transport, err := networking.GetHttpTransport(config_obj.Client, arg.RootCerts)
		if err != nil {
			scope.Log("humio: %v", err)
			return
		}

		if arg.SkipVerify {
			err = networking.EnableSkipVerify(transport.TLSClientConfig,
							  config_obj.Client)
			if err != nil {
				scope.Log("humio: %v", err)
				return
			}
		}

		err = queue.SetHttpTransport(transport)
		if err != nil {
			scope.Log("humio: %v", err)
			return
		}

		err = queue.Open(scope, arg.ApiBaseUrl, arg.IngestToken)
		if err != nil {
			scope.Log("humio: could not open queue: %v", err)
			return
		}
		queue.Log(scope, "plugin started (%v threads)", queue.WorkerCount())
		defer queue.Log(scope, "plugin terminating")
		defer queue.Close(scope)

		rowChan := arg.Query.Eval(ctx, scope)
		finished := false

		ticker := time.NewTicker(gStatsLogPeriod)
		for {
			if finished {
				break
			}
			select {
			case <- ctx.Done():
				finished = true
				break
			case row, ok := <- rowChan:
				if !ok {
					finished = true
					break
				}
				rowData := vfilter.RowToDict(ctx, scope, row)
				queue.QueueEvent(rowData)
			case <-ticker.C:
				queue.PostBacklogStats(scope)
				ticker.Reset(gStatsLogPeriod)
			}
		}
	}()
	return outputChan
}

func (self humioPlugin) Info(
	scope vfilter.Scope,
	type_map *vfilter.TypeMap) *vfilter.PluginInfo {
	return &vfilter.PluginInfo{
		Name: "humio_upload",
		Doc:  "Upload rows to Humio ingestion server.",

		ArgType: type_map.AddType(scope, &humioPluginArgs{}),
	}
}

func init() {
	vql_subsystem.RegisterPlugin(&humioPlugin{})
}
