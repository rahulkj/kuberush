package exporter

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

type CloudWatchLogsExporter struct {
	client        *cloudwatchlogs.Client
	logGroup      string
	logStream     string
	mu            sync.Mutex
	sequenceToken *string
	initOnce      sync.Once
	initErr       error
}

func NewCloudWatchLogsExporter(ctx context.Context, region, endpoint, logGroup, logStream string) (*CloudWatchLogsExporter, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	cwOpts := []func(*cloudwatchlogs.Options){}
	if endpoint != "" {
		cwOpts = append(cwOpts, func(o *cloudwatchlogs.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}
	if logStream == "" {
		logStream = "kuberush"
	}
	return &CloudWatchLogsExporter{
		client:    cloudwatchlogs.NewFromConfig(cfg, cwOpts...),
		logGroup:  logGroup,
		logStream: logStream,
	}, nil
}

func (e *CloudWatchLogsExporter) Name() string { return "cloudwatch-logs" }

func (e *CloudWatchLogsExporter) ensure(ctx context.Context) error {
	e.initOnce.Do(func() {
		if e.logGroup == "" {
			e.initErr = fmt.Errorf("cloudwatch log group is required")
			return
		}
		_, err := e.client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String(e.logGroup)})
		if err != nil && !isResourceExists(err) {
			// ignore ResourceAlreadyExistsException
			if !strings.Contains(err.Error(), "ResourceAlreadyExists") {
				// continue; may already exist or be handled
			}
		}
		_, err = e.client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
			LogGroupName:  aws.String(e.logGroup),
			LogStreamName: aws.String(e.logStream),
		})
		if err != nil && !strings.Contains(err.Error(), "ResourceAlreadyExists") {
			// best effort; stream may exist
		}
	})
	return e.initErr
}

func (e *CloudWatchLogsExporter) WriteLogs(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	if err := e.ensure(ctx); err != nil {
		return err
	}
	events := make([]types.InputLogEvent, 0, len(records))
	now := time.Now().UnixMilli()
	for i, r := range records {
		msg := strings.TrimRight(r.Message, "\n")
		if msg == "" {
			msg = "(empty)"
		}
		// CloudWatch requires timestamps in ms and non-empty message
		ts := now + int64(i)
		events = append(events, types.InputLogEvent{
			Message:   aws.String(fmt.Sprintf("namespace=%s pod=%s container=%s node=%s %s", r.Namespace, r.Pod, r.Container, r.NodeName, msg)),
			Timestamp: aws.Int64(ts),
		})
	}
	sort.Slice(events, func(i, j int) bool { return aws.ToInt64(events[i].Timestamp) < aws.ToInt64(events[j].Timestamp) })

	e.mu.Lock()
	defer e.mu.Unlock()

	input := &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String(e.logGroup),
		LogStreamName: aws.String(e.logStream),
		LogEvents:     events,
		SequenceToken: e.sequenceToken,
	}
	out, err := e.client.PutLogEvents(ctx, input)
	if err != nil {
		// Try to recover from sequence token issues
		if strings.Contains(err.Error(), "InvalidSequenceToken") || strings.Contains(err.Error(), "DataAlreadyAccepted") {
			e.sequenceToken = nil
			input.SequenceToken = nil
			out, err = e.client.PutLogEvents(ctx, input)
		}
	}
	if err != nil {
		return fmt.Errorf("put cloudwatch log events: %w", err)
	}
	if out != nil {
		e.sequenceToken = out.NextSequenceToken
	}
	return nil
}

func (e *CloudWatchLogsExporter) Close() error { return nil }

func isResourceExists(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "ResourceAlreadyExists")
}
