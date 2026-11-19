package kube

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func BuildClientset(kubeconfig string) (*rest.Config, kubernetes.Interface, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			overrides := &clientcmd.ConfigOverrides{}
			kubeCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
			cfg, err = kubeCfg.ClientConfig()
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("build kube config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create clientset: %w", err)
	}
	return cfg, cs, nil
}
