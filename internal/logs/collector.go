package logs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/rahulkj/kubemon/internal/exporter"
)

type Collector struct {
	client kubernetes.Interface
	sinks  []exporter.LogSink

	mu      sync.Mutex
	running map[string]context.CancelFunc // key: ns/pod/container
}

func NewCollector(client kubernetes.Interface, sinks []exporter.LogSink) *Collector {
	return &Collector{
		client:  client,
		sinks:   sinks,
		running: make(map[string]context.CancelFunc),
	}
}

func (c *Collector) key(ns, pod, container string) string {
	return ns + "/" + pod + "/" + container
}

func (c *Collector) HandlePod(pod *corev1.Pod) {
	if pod.Status.Phase != corev1.PodRunning {
		return
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Running == nil {
			continue
		}
		c.startTailer(pod.Namespace, pod.Name, cs.Name)
	}
}

func (c *Collector) StopPod(ns, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := ns + "/" + name + "/"
	for k, cancel := range c.running {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			cancel()
			delete(c.running, k)
		}
	}
}

func (c *Collector) startTailer(ns, podName, container string) {
	k := c.key(ns, podName, container)
	c.mu.Lock()
	if _, exists := c.running[k]; exists {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.running[k] = cancel
	c.mu.Unlock()

	go c.tailContainer(ctx, ns, podName, container, k)
}

func (c *Collector) tailContainer(ctx context.Context, ns, podName, container, k string) {
	defer func() {
		c.mu.Lock()
		delete(c.running, k)
		c.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.streamLogs(ctx, ns, podName, container)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			fmt.Printf("[logtail] error %s/%s/%s: %v (retrying in 5s)\n", ns, podName, container, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		return
	}
}

func (c *Collector) streamLogs(ctx context.Context, ns, podName, container string) error {
	opts := &corev1.PodLogOptions{
		Container:  container,
		Follow:     true,
		Timestamps: true,
	}
	req := c.client.CoreV1().Pods(ns).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			rec := exporter.LogRecord{
				Namespace: ns,
				Pod:       podName,
				Container: container,
				Message:   line,
			}
			for _, sink := range c.sinks {
				_ = sink.WriteLogs(ctx, []exporter.LogRecord{rec})
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
