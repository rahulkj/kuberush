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
	return &PodWatcher{clientset: clientset, factory: factory, handler: handler, kubeCfg: kubeCfg}
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
