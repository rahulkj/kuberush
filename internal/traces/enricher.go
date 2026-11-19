package traces

import (
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	corev1 "k8s.io/api/core/v1"
)

type PodLookup interface {
	Lookup(namespace, name string) (*corev1.Pod, bool)
	LookupByIP(ip string) (*corev1.Pod, bool)
}

type Enricher struct {
	lookup PodLookup
}

func NewEnricher(lookup PodLookup) *Enricher {
	return &Enricher{lookup: lookup}
}

func (e *Enricher) Enrich(req *coltracepb.ExportTraceServiceRequest) *coltracepb.ExportTraceServiceRequest {
	if req == nil || e == nil || e.lookup == nil {
		return req
	}
	for _, rs := range req.ResourceSpans {
		if rs == nil {
			continue
		}
		if rs.Resource == nil {
			rs.Resource = &resourcepb.Resource{}
		}
		pod := e.findPod(rs)
		if pod != nil {
			enrichResource(rs.Resource, pod)
		}
	}
	return req
}

func (e *Enricher) findPod(rs *tracepb.ResourceSpans) *corev1.Pod {
	attrs := map[string]string{}
	if rs.Resource != nil {
		for _, kv := range rs.Resource.Attributes {
			if kv == nil || kv.Key == "" || kv.Value == nil {
				continue
			}
			if sv, ok := kv.Value.Value.(*commonpb.AnyValue_StringValue); ok {
				attrs[kv.Key] = sv.StringValue
			}
		}
	}

	if ns, name := attrs["k8s.namespace.name"], attrs["k8s.pod.name"]; ns != "" && name != "" {
		if pod, ok := e.lookup.Lookup(ns, name); ok {
			return pod
		}
	}
	for _, key := range []string{"k8s.pod.ip", "net.host.ip", "host.ip", "ip"} {
		if ip := attrs[key]; ip != "" {
			if pod, ok := e.lookup.LookupByIP(ip); ok {
				return pod
			}
		}
	}
	return nil
}

func enrichResource(res *resourcepb.Resource, pod *corev1.Pod) {
	setIfMissing(res, "k8s.namespace.name", pod.Namespace)
	setIfMissing(res, "k8s.pod.name", pod.Name)
	setIfMissing(res, "k8s.pod.uid", string(pod.UID))
	if pod.Spec.NodeName != "" {
		setIfMissing(res, "k8s.node.name", pod.Spec.NodeName)
	}
	if pod.Status.PodIP != "" {
		setIfMissing(res, "k8s.pod.ip", pod.Status.PodIP)
	}
	for k, v := range pod.Labels {
		if k == "" {
			continue
		}
		setIfMissing(res, "k8s.pod.label."+k, v)
	}
}

func setIfMissing(res *resourcepb.Resource, key, value string) {
	if res == nil || key == "" || value == "" {
		return
	}
	for _, kv := range res.Attributes {
		if kv != nil && kv.Key == key {
			return
		}
	}
	res.Attributes = append(res.Attributes, &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
	})
}
