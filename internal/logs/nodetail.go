package logs

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rahulkj/kubemon/internal/config"
	"github.com/rahulkj/kubemon/internal/exporter"
	"github.com/rahulkj/kubemon/internal/selfmetrics"
)

type PodMetaLookup interface {
	Lookup(namespace, name string) (nodeName string, labels map[string]string, ok bool)
}

type DiscoveryLookup interface {
	Lookup(namespace, name string) (interface{ GetNodeName() string }, bool)
}

// Simple lookup adapter via function to avoid tight coupling; we use a concrete helper in main if needed.
// Instead, define a minimal interface matching what we need.
type NodeTailPodLookup interface {
	LookupPodMeta(namespace, name string) (nodeName string, labels map[string]string, ok bool)
}

type NodeTailer struct {
	root     string
	sinks    []exporter.LogSink
	metrics  *selfmetrics.Metrics
	kubeCfg  config.KubernetesConfig
	sharding config.ShardingConfig
	lookup   NodeTailPodLookup

	mu      sync.Mutex
	offsets map[string]int64
}

func NewNodeTailer(root string, sinks []exporter.LogSink, sm *selfmetrics.Metrics, kubeCfg config.KubernetesConfig, sharding config.ShardingConfig, lookup NodeTailPodLookup) *NodeTailer {
	if root == "" {
		root = "/var/log/pods"
	}
	return &NodeTailer{
		root:     root,
		sinks:    sinks,
		metrics:  sm,
		kubeCfg:  kubeCfg,
		sharding: sharding,
		lookup:   lookup,
		offsets:  make(map[string]int64),
	}
}

func (n *NodeTailer) Run(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		n.scanOnce(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (n *NodeTailer) scanOnce(ctx context.Context) {
	// Expected path: /var/log/pods/<namespace>_<pod>_<uid>/<container>/<n>.log
	entries, err := os.ReadDir(n.root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ns, podName, ok := parsePodDir(e.Name())
		if !ok {
			continue
		}
		// labels unknown here; sharding/filter by namespace+name is best-effort
		if !n.kubeCfg.PodAllowed(ns, podName, map[string]string{}, n.sharding) {
			continue
		}
		podDir := filepath.Join(n.root, e.Name())
		containers, err := os.ReadDir(podDir)
		if err != nil {
			continue
		}
		for _, c := range containers {
			if !c.IsDir() {
				continue
			}
			cdir := filepath.Join(podDir, c.Name())
			files, err := filepath.Glob(filepath.Join(cdir, "*.log"))
			if err != nil {
				continue
			}
			for _, f := range files {
				n.tailFile(ctx, ns, podName, c.Name(), f)
			}
		}
	}
}

func (n *NodeTailer) tailFile(ctx context.Context, ns, podName, container, path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	n.mu.Lock()
	off, seen := n.offsets[path]
	if !seen || fi.Size() < off {
		// start at end for existing files on first sight to avoid flooding historical logs
		if !seen && fi.Size() > 0 {
			off = fi.Size()
		} else {
			off = 0
		}
	}
	n.mu.Unlock()

	if fi.Size() <= off {
		n.mu.Lock()
		n.offsets[path] = fi.Size()
		n.mu.Unlock()
		return
	}

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return
	}
	reader := bufio.NewScanner(f)
	// allow reasonably long log lines
	buf := make([]byte, 64*1024)
	reader.Buffer(buf, 1024*1024)

	var nodeName string
	var labels map[string]string
	if n.lookup != nil {
		nodeName, labels, _ = n.lookup.LookupPodMeta(ns, podName)
	}

	for reader.Scan() {
		line := reader.Text()
		msg := parseNodeLogLine(line)
		rec := exporter.LogRecord{
			Namespace: ns,
			Pod:       podName,
			Container: container,
			NodeName:  nodeName,
			PodLabels: labels,
			Message:   msg,
		}
		for _, sink := range n.sinks {
			if err := sink.WriteLogs(ctx, []exporter.LogRecord{rec}); err != nil {
				if n.metrics != nil {
					n.metrics.IncLogExportErrors(sink.Name())
				}
			} else if n.metrics != nil {
				n.metrics.IncLogRecords(sink.Name(), 1)
			}
		}
	}
	n.mu.Lock()
	n.offsets[path], _ = f.Seek(0, io.SeekCurrent)
	// if scanner stopped due to token too long etc., advance to file size to avoid tight loop
	if st, err := f.Stat(); err == nil && n.offsets[path] < st.Size() && reader.Err() != nil {
		n.offsets[path] = st.Size()
	}
	n.mu.Unlock()
}

func parsePodDir(name string) (namespace, pod string, ok bool) {
	// <namespace>_<pod>_<uid>
	parts := strings.Split(name, "_")
	if len(parts) < 3 {
		return "", "", false
	}
	namespace = parts[0]
	// pod name may contain underscores? uncommon, but safer to join middle parts except last (uid)
	pod = strings.Join(parts[1:len(parts)-1], "_")
	if namespace == "" || pod == "" {
		return "", "", false
	}
	return namespace, pod, true
}

func parseNodeLogLine(line string) string {
	// CRI format: <timestamp> <stream> <P/F> <message>
	// If not matching, return original line.
	parts := strings.SplitN(line, " ", 4)
	if len(parts) == 4 {
		// basic heuristic: stream stdout/stderr and flag P/F
		if (parts[1] == "stdout" || parts[1] == "stderr") && (parts[2] == "P" || parts[2] == "F") {
			return parts[3] + "\n"
		}
	}
	if !strings.HasSuffix(line, "\n") {
		return line + "\n"
	}
	return line
}
