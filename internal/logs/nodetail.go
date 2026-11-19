package logs

import (
	"bufio"
	"context"
	"encoding/json"
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

type fileState struct {
	Offset int64  `json:"offset"`
	Dev    uint64 `json:"dev,omitempty"`
	Ino    uint64 `json:"ino,omitempty"`
}

type NodeTailer struct {
	root           string
	checkpointPath string
	nodeName       string
	sinks          []exporter.LogSink
	metrics        *selfmetrics.Metrics
	kubeCfg        config.KubernetesConfig
	sharding       config.ShardingConfig
	lookup         NodeTailPodLookup

	mu       sync.Mutex
	states   map[string]fileState
	lastSave time.Time
}

func NewNodeTailer(root, checkpointPath, nodeName string, sinks []exporter.LogSink, sm *selfmetrics.Metrics, kubeCfg config.KubernetesConfig, sharding config.ShardingConfig, lookup NodeTailPodLookup) *NodeTailer {
	if root == "" {
		root = "/var/log/pods"
	}
	n := &NodeTailer{
		root:           root,
		checkpointPath: checkpointPath,
		nodeName:       nodeName,
		sinks:          sinks,
		metrics:        sm,
		kubeCfg:        kubeCfg,
		sharding:       sharding,
		lookup:         lookup,
		states:         make(map[string]fileState),
	}
	n.loadCheckpoints()
	return n
}

func (n *NodeTailer) Run(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		n.scanOnce(ctx)
		n.maybeSaveCheckpoints(false)
		select {
		case <-ctx.Done():
			n.maybeSaveCheckpoints(true)
			return nil
		case <-ticker.C:
		}
	}
}

func (n *NodeTailer) loadCheckpoints() {
	if n.checkpointPath == "" {
		return
	}
	data, err := os.ReadFile(n.checkpointPath)
	if err != nil {
		return
	}
	// Try new format first
	var m map[string]fileState
	if err := json.Unmarshal(data, &m); err == nil {
		n.mu.Lock()
		for k, v := range m {
			n.states[k] = v
		}
		n.mu.Unlock()
		return
	}
	// Backward compat: old format map[string]int64
	var old map[string]int64
	if err := json.Unmarshal(data, &old); err == nil {
		n.mu.Lock()
		for k, v := range old {
			n.states[k] = fileState{Offset: v}
		}
		n.mu.Unlock()
	}
}

func (n *NodeTailer) maybeSaveCheckpoints(force bool) {
	if n.checkpointPath == "" {
		return
	}
	n.mu.Lock()
	if !force && time.Since(n.lastSave) < 10*time.Second {
		n.mu.Unlock()
		return
	}
	snapshot := make(map[string]fileState, len(n.states))
	for k, v := range n.states {
		snapshot[k] = v
	}
	n.lastSave = time.Now()
	n.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(n.checkpointPath), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	tmp := n.checkpointPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, n.checkpointPath)
}

func (n *NodeTailer) pruneCheckpoints(seen map[string]struct{}) {
	if n.checkpointPath == "" {
		return
	}
	n.mu.Lock()
	changed := false
	for k := range n.states {
		if _, ok := seen[k]; !ok {
			delete(n.states, k)
			changed = true
		}
	}
	n.mu.Unlock()
	if changed {
		n.maybeSaveCheckpoints(true)
	}
}

func (n *NodeTailer) scanOnce(ctx context.Context) {
	// Expected path: /var/log/pods/<namespace>_<pod>_<uid>/<container>/<n>.log
	entries, err := os.ReadDir(n.root)
	if err != nil {
		return
	}
	seen := map[string]struct{}{}
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
				seen[f] = struct{}{}
				n.tailFile(ctx, ns, podName, c.Name(), f)
			}
		}
	}
	n.pruneCheckpoints(seen)
}

func (n *NodeTailer) tailFile(ctx context.Context, ns, podName, container, path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	id := fileIdentity(fi)

	n.mu.Lock()
	st, seen := n.states[path]
	if seen && (st.Dev != id.Dev || st.Ino != id.Ino) {
		// rotated/recreated file
		st = fileState{Dev: id.Dev, Ino: id.Ino}
		seen = false
	}
	off := st.Offset
	if !seen || fi.Size() < off {
		if !seen && fi.Size() > 0 {
			off = fi.Size()
		} else {
			off = 0
		}
	}
	n.mu.Unlock()

	var nodeName string
	var labels map[string]string
	if n.lookup != nil {
		nodeName, labels, _ = n.lookup.LookupPodMeta(ns, podName)
	}
	if n.nodeName != "" && nodeName != "" && nodeName != n.nodeName {
		// per-node filtering guarantee when pod metadata is available
		n.mu.Lock()
		n.states[path] = fileState{Offset: fi.Size(), Dev: id.Dev, Ino: id.Ino}
		n.mu.Unlock()
		return
	}

	if fi.Size() <= off {
		n.mu.Lock()
		n.states[path] = fileState{Offset: fi.Size(), Dev: id.Dev, Ino: id.Ino}
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
	newOff, _ := f.Seek(0, io.SeekCurrent)
	n.mu.Lock()
	// if scanner stopped due to token too long etc., advance to file size to avoid tight loop
	if stfi, err := f.Stat(); err == nil && newOff < stfi.Size() && reader.Err() != nil {
		newOff = stfi.Size()
	}
	n.states[path] = fileState{Offset: newOff, Dev: id.Dev, Ino: id.Ino}
	n.mu.Unlock()
}

type fileID struct {
	Dev uint64
	Ino uint64
}

func fileIdentity(fi os.FileInfo) fileID {
	return fileIDFromSys(fi.Sys())
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
	// Docker JSON format: {"log":"...","stream":"stdout","time":"..."}
	if strings.HasPrefix(line, "{") && strings.Contains(line, "\"log\"") {
		var obj struct {
			Log string `json:"log"`
		}
		if err := json.Unmarshal([]byte(line), &obj); err == nil {
			if strings.HasSuffix(obj.Log, "\n") {
				return obj.Log
			}
			return obj.Log + "\n"
		}
	}
	parts := strings.SplitN(line, " ", 4)
	if len(parts) == 4 {
		// basic heuristic: stream stdout/stderr and flag P/F
		if (parts[1] == "stdout" || parts[1] == "stderr") && (parts[2] == "P" || parts[2] == "F") {
			msg := parts[3]
			if parts[2] == "P" {
				// partial line; keep as-is without forcing newline
				return msg
			}
			if !strings.HasSuffix(msg, "\n") {
				return msg + "\n"
			}
			return msg
		}
	}
	if !strings.HasSuffix(line, "\n") {
		return line + "\n"
	}
	return line
}
