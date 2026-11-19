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
	"github.com/rahulkj/kubemon/internal/selfmetrics"
)

type podInfo struct {
	NodeName string
	Labels   map[string]string
}

type Collector struct {
	client  kubernetes.Interface
	sinks   []exporter.LogSink
	metrics *selfmetrics.Metrics

	mu      sync.Mutex
	running map[string]context.CancelFunc // key: ns/pod/container
	pods    map[string]podInfo            // key: ns/pod
}

func NewCollector(client kubernetes.Interface, sinks []exporter.LogSink, metrics *selfmetrics.Metrics) *Collector {
	return &Collector{
		client:  client,
		sinks:   sinks,
		metrics: metrics,
		running: make(map[string]context.CancelFunc),
		pods:    make(map[string]podInfo),
	}
}

func (c *Collector) key(ns, pod, container string) string {
	return ns + "/" + pod + "/" + container
}

func (c *Collector) podKey(ns, pod string) string {
	return ns + "/" + pod
}

func (c *Collector) HandlePod(pod *corev1.Pod) {
	c.mu.Lock()
	c.pods[c.podKey(pod.Namespace, pod.Name)] = podInfo{NodeName: pod.Spec.NodeName, Labels: pod.Labels}
	c.mu.Unlock()

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
	delete(c.pods, c.podKey(ns, name))
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
			if c.metrics != nil {
				c.metrics.IncLogTailerErrors()
			}
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
			c.mu.Lock()
			info := c.pods[c.podKey(ns, podName)]
			c.mu.Unlock()
			rec := exporter.LogRecord{
				Namespace: ns,
				Pod:       podName,
				Container: container,
				NodeName:  info.NodeName,
				PodLabels: info.Labels,
				Message:   line,
			}
			for _, sink := range c.sinks {
				if werr := sink.WriteLogs(ctx, []exporter.LogRecord{rec}); werr != nil {
					if c.metrics != nil {
						c.metrics.IncLogExportErrors(sink.Name())
					}
				} else if c.metrics != nil {
					c.metrics.IncLogRecords(sink.Name(), 1)
				}
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
