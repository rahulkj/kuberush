package discovery

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

type PodWatcher struct {
	clientset kubernetes.Interface
	factory   informers.SharedInformerFactory
}

func NewPodWatcher(kubeconfig string) (*PodWatcher, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			// fallback to default kubeconfig
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			overrides := &clientcmd.ConfigOverrides{}
			kubeCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
			cfg, err = kubeCfg.ClientConfig()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("build kube config: %w", err)
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	factory := informers.NewSharedInformerFactory(cs, 30*time.Second)
	return &PodWatcher{clientset: cs, factory: factory}, nil
}

func (pw *PodWatcher) Run(ctx context.Context) error {
	podInformer := pw.factory.Core().V1().Pods().Informer()
	_, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if pod, ok := obj.(*corev1.Pod); ok {
				fmt.Printf("[pod:add] %s/%s phase=%s\n", pod.Namespace, pod.Name, pod.Status.Phase)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if pod, ok := newObj.(*corev1.Pod); ok {
				fmt.Printf("[pod:update] %s/%s phase=%s\n", pod.Namespace, pod.Name, pod.Status.Phase)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if pod, ok := obj.(*corev1.Pod); ok {
				fmt.Printf("[pod:delete] %s/%s\n", pod.Namespace, pod.Name)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("add event handler: %w", err)
	}

	pw.factory.Start(ctx.Done())
	// wait for cache sync
	for t, ok := range pw.factory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return fmt.Errorf("failed to sync informer for %v", t)
		}
	}
	fmt.Println("pod informer synced (watching all namespaces)")
	<-ctx.Done()
	return nil
}
