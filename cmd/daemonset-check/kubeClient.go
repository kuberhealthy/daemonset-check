package main

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// kubeConfigDirName is the default directory for kubeconfig files.
	kubeConfigDirName = ".kube"
	// kubeConfigFileName is the default kubeconfig filename.
	kubeConfigFileName = "config"
)

// createKubeClient builds a Kubernetes clientset from in-cluster or local config.
func createKubeClient() (*kubernetes.Clientset, error) {
	// Attempt to use the in-cluster configuration first.
	config, err := rest.InClusterConfig()
	if err == nil {
		clientset, clientErr := kubernetes.NewForConfig(config)
		if clientErr != nil {
			return nil, fmt.Errorf("failed to create in-cluster client: %w", clientErr)
		}
		return clientset, nil
	}

	// Fall back to a local kubeconfig file for development.
	kubeconfigPath, pathErr := kubeConfigPath()
	if pathErr != nil {
		return nil, pathErr
	}

	kubeconfig, configErr := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if configErr != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", configErr)
	}

	clientset, clientErr := kubernetes.NewForConfig(kubeconfig)
	if clientErr != nil {
		return nil, fmt.Errorf("failed to create kube client: %w", clientErr)
	}

	return clientset, nil
}

// kubeConfigPath determines the kubeconfig file path for local use.
func kubeConfigPath() (string, error) {
	// Prefer HOME for consistency with other checks.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}

	return filepath.Join(homeDir, kubeConfigDirName, kubeConfigFileName), nil
}
