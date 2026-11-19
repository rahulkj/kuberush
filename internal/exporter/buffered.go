package exporter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type BufferedLogSink struct {
	next          LogSink
	batchSize     int
	flushInterval time.Duration
	maxRetries    int
	retryBackoff  time.Duration
	dropPolicy    string
	queue         chan LogRecord
	stop          chan struct{}
	done          chan struct{}
	closeOnce     sync.Once
	dropped       uint64
	mu            sync.Mutex
}

func NewBufferedLogSink(next LogSink, batchSize int, flushInterval time.Duration, queueSize, maxRetries int, retryBackoff time.Duration, dropPolicy string) *BufferedLogSink {
	if batchSize <= 0 {
		batchSize = 200
	}
	if flushInterval <= 0 {
		flushInterval = 2 * time.Second
	}
	if queueSize <= 0 {
		queueSize = 10000
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if retryBackoff <= 0 {
		retryBackoff = 500 * time.Millisecond
	}
	if dropPolicy == "" {
		dropPolicy = "drop_newest"
	}
	b := &BufferedLogSink{
		next:          next,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		maxRetries:    maxRetries,
		retryBackoff:  retryBackoff,
		dropPolicy:    dropPolicy,
		queue:         make(chan LogRecord, queueSize),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	go b.run()
	return b
}

func (b *BufferedLogSink) Name() string { return b.next.Name() + "+buffered" }

func (b *BufferedLogSink) WriteLogs(ctx context.Context, records []LogRecord) error {
	for _, r := range records {
		select {
		case b.queue <- r:
		default:
			if b.dropPolicy == "drop_oldest" {
				select {
				case <-b.queue:
				default:
				}
				select {
				case b.queue <- r:
					continue
				default:
				}
			}
			b.mu.Lock()
			b.dropped++
			d := b.dropped
			b.mu.Unlock()
			if d == 1 || d%1000 == 0 {
				fmt.Printf("[exporter:%s] dropping log records due to full queue policy=%s (dropped=%d)\n", b.next.Name(), b.dropPolicy, d)
			}
		}
	}
	return nil
}

func (b *BufferedLogSink) run() {
	defer close(b.done)
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	batch := make([]LogRecord, 0, b.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		out := make([]LogRecord, len(batch))
		copy(out, batch)
		batch = batch[:0]
		var err error
		for attempt := 0; attempt < b.maxRetries; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err = b.next.WriteLogs(ctx, out)
			cancel()
			if err == nil {
				return
			}
			if attempt < b.maxRetries-1 {
				time.Sleep(time.Duration(attempt+1) * b.retryBackoff)
			}
		}
		if err != nil {
			fmt.Printf("[exporter:%s] flush error after %d retries: %v\n", b.next.Name(), b.maxRetries, err)
		}
	}

	for {
		select {
		case r, ok := <-b.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, r)
			if len(batch) >= b.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-b.stop:
			for {
				select {
				case r := <-b.queue:
					batch = append(batch, r)
					if len(batch) >= b.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (b *BufferedLogSink) Close() error {
	b.closeOnce.Do(func() {
		close(b.stop)
		<-b.done
		_ = b.next.Close()
	})
	return nil
}
