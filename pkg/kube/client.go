// Package kube provides utilities for interacting with Kubernetes
package kube

import (
	"fmt"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	clientset     *kubernetes.Clientset
	clientsetOnce sync.Once
	clientsetErr  error
	dynamicClient dynamic.Interface
	dynamicOnce   sync.Once
	dynamicErr    error
)

// GetClientset returns a Kubernetes clientset for interacting with the API server
// It uses a singleton pattern to ensure only one client is created
func GetClientset() (*kubernetes.Clientset, error) {
	clientsetOnce.Do(func() {
		config, err := rest.InClusterConfig()
		if err != nil {
			clientsetErr = fmt.Errorf("failed to get in-cluster config: %v", err)
			return
		}

		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			clientsetErr = fmt.Errorf("failed to create Kubernetes client: %v", err)
			return
		}
	})

	return clientset, clientsetErr
}

// GetDynamicClient returns a dynamic client for interacting with custom resources
// It uses a singleton pattern to ensure only one client is created
func GetDynamicClient() (dynamic.Interface, error) {
	dynamicOnce.Do(func() {
		config, err := rest.InClusterConfig()
		if err != nil {
			dynamicErr = fmt.Errorf("failed to get in-cluster config: %v", err)
			return
		}

		dynamicClient, err = dynamic.NewForConfig(config)
		if err != nil {
			dynamicErr = fmt.Errorf("failed to create dynamic client: %v", err)
			return
		}
	})

	return dynamicClient, dynamicErr
}
