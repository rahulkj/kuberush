package discovery

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/rahulkj/kubemon/internal/config"
)

type PodHandler interface {
	OnPodAdd(pod *corev1.Pod)
	OnPodUpdate(pod *corev1.Pod)
	OnPodDelete(pod *corev1.Pod)
}

type PodWatcher struct {
	clientset kubernetes.Interface
	factory   informers.SharedInformerFactory
	lister    corelisters.PodLister
	handler   PodHandler
	kubeCfg   config.KubernetesConfig
}

func NewPodWatcher(clientset kubernetes.Interface, handler PodHandler, kubeCfg config.KubernetesConfig) *PodWatcher {
	var opts []informers.SharedInformerOption
	if kubeCfg.LabelSelector != "" {
		if _, err := labels.Parse(kubeCfg.LabelSelector); err == nil {
			ls := kubeCfg.LabelSelector
			opts = append(opts, informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = ls
			}))
		}
	}
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 30*time.Second, opts...)
	return &PodWatcher{
		clientset: clientset,
		factory:   factory,
		lister:    factory.Core().V1().Pods().Lister(),
		handler:   handler,
		kubeCfg:   kubeCfg,
	}
}

func (pw *PodWatcher) Lookup(namespace, name string) (*corev1.Pod, bool) {
	if pw.lister == nil || namespace == "" || name == "" {
		return nil, false
	}
	pod, err := pw.lister.Pods(namespace).Get(name)
	if err != nil || pod == nil {
		return nil, false
	}
	if !pw.kubeCfg.PodAllowed(pod.Namespace, pod.Labels) {
		return nil, false
	}
	return pod, true
}

func (pw *PodWatcher) LookupByIP(ip string) (*corev1.Pod, bool) {
	if pw.lister == nil || ip == "" {
		return nil, false
	}
	pods, err := pw.lister.List(labels.Everything())
	if err != nil {
		return nil, false
	}
	for _, pod := range pods {
		if pod == nil || !pw.kubeCfg.PodAllowed(pod.Namespace, pod.Labels) {
			continue
		}
		if pod.Status.PodIP == ip {
			return pod, true
		}
		for _, pip := range pod.Status.PodIPs {
			if pip.IP == ip {
				return pod, true
			}
		}
	}
	return nil, false
}

func (pw *PodWatcher) Run(ctx context.Context) error {
	podInformer := pw.factory.Core().V1().Pods().Informer()
	_, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if pod, ok := obj.(*corev1.Pod); ok {
				if !pw.kubeCfg.PodAllowed(pod.Namespace, pod.Labels) {
					return
				}
				fmt.Printf("[pod:add] %s/%s phase=%s\n", pod.Namespace, pod.Name, pod.Status.Phase)
				if pw.handler != nil {
					pw.handler.OnPodAdd(pod)
				}
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if pod, ok := newObj.(*corev1.Pod); ok {
				if !pw.kubeCfg.PodAllowed(pod.Namespace, pod.Labels) {
					if pw.handler != nil {
						pw.handler.OnPodDelete(pod)
					}
					return
				}
				fmt.Printf("[pod:update] %s/%s phase=%s\n", pod.Namespace, pod.Name, pod.Status.Phase)
				if pw.handler != nil {
					pw.handler.OnPodUpdate(pod)
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			if pod, ok := obj.(*corev1.Pod); ok {
				fmt.Printf("[pod:delete] %s/%s\n", pod.Namespace, pod.Name)
				if pw.handler != nil {
					pw.handler.OnPodDelete(pod)
				}
			}
		},
	})
	if err != nil {
		return fmt.Errorf("add event handler: %w", err)
	}

	pw.factory.Start(ctx.Done())
	for t, ok := range pw.factory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return fmt.Errorf("failed to sync informer for %v", t)
		}
	}
	fmt.Println("pod informer synced (watching configured namespaces/pods)")
	<-ctx.Done()
	return nil
}
