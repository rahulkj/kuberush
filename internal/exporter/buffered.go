package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	dlqDir        string
	spoolDir      string
	queue         chan LogRecord
	stop          chan struct{}
	done          chan struct{}
	closeOnce     sync.Once
	dropped       uint64
	mu            sync.Mutex
}

func NewBufferedLogSink(next LogSink, batchSize int, flushInterval time.Duration, queueSize, maxRetries int, retryBackoff time.Duration, dropPolicy, dlqDir, spoolDir string) *BufferedLogSink {
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
		dlqDir:        strings.TrimSpace(dlqDir),
		spoolDir:      strings.TrimSpace(spoolDir),
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
	b.replaySpool()

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
		b.flushBatch(out)
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

func (b *BufferedLogSink) flushBatch(out []LogRecord) {
	spoolFile := b.writeSpoolPending(out)
	var err error
	for attempt := 0; attempt < b.maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = b.next.WriteLogs(ctx, out)
		cancel()
		if err == nil {
			if spoolFile != "" {
				_ = os.Remove(spoolFile)
			}
			return
		}
		if attempt < b.maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * b.retryBackoff)
		}
	}
	if err != nil {
		fmt.Printf("[exporter:%s] flush error after %d retries: %v\n", b.next.Name(), b.maxRetries, err)
		b.handleFailedBatch(out, spoolFile, err)
	}
}

func (b *BufferedLogSink) sinkDirName() string {
	name := b.next.Name()
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "+", "_")
	if name == "" {
		name = "sink"
	}
	return name
}

func (b *BufferedLogSink) writeSpoolPending(batch []LogRecord) string {
	if b.spoolDir == "" {
		return ""
	}
	dir := filepath.Join(b.spoolDir, b.sinkDirName(), "pending")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, fmt.Sprintf("%d-%d.json", time.Now().UnixNano(), time.Now().Unix()))
	data, err := json.Marshal(batch)
	if err != nil {
		return ""
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return ""
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return path
}

func (b *BufferedLogSink) handleFailedBatch(batch []LogRecord, spoolFile string, flushErr error) {
	if b.dlqDir != "" {
		dir := filepath.Join(b.dlqDir, b.sinkDirName())
		if err := os.MkdirAll(dir, 0o755); err == nil {
			path := filepath.Join(dir, fmt.Sprintf("%d.json", time.Now().UnixNano()))
			payload := map[string]any{
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
				"sink":      b.next.Name(),
				"error":     flushErr.Error(),
				"records":   batch,
			}
			data, err := json.Marshal(payload)
			if err == nil {
				tmp := path + ".tmp"
				if err := os.WriteFile(tmp, data, 0o644); err == nil {
					if err := os.Rename(tmp, path); err == nil {
						if spoolFile != "" {
							_ = os.Remove(spoolFile)
						}
						return
					}
					_ = os.Remove(tmp)
				}
			}
		}
	}
	// If no DLQ or DLQ write failed, keep spool pending file (if any) for replay on restart.
	_ = spoolFile
}

func (b *BufferedLogSink) replaySpool() {
	if b.spoolDir == "" {
		return
	}
	dir := filepath.Join(b.spoolDir, b.sinkDirName(), "pending")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var batch []LogRecord
		if err := json.Unmarshal(data, &batch); err != nil {
			continue
		}
		if len(batch) == 0 {
			_ = os.Remove(path)
			continue
		}
		// try to flush replayed batch; if it fails, handleFailedBatch may move to DLQ or keep pending
		var ferr error
		for attempt := 0; attempt < b.maxRetries; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			ferr = b.next.WriteLogs(ctx, batch)
			cancel()
			if ferr == nil {
				_ = os.Remove(path)
				break
			}
			if attempt < b.maxRetries-1 {
				time.Sleep(time.Duration(attempt+1) * b.retryBackoff)
			}
		}
		if ferr != nil {
			b.handleFailedBatch(batch, path, ferr)
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
