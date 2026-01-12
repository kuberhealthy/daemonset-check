package main

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// podAPIVersion is used for owner reference metadata.
	podAPIVersion = "v1"
	// podKind is used for owner reference metadata.
	podKind = "Pod"
)

// getOwnerRef fetches the current pod UID and returns an owner reference list.
func getOwnerRef(client *kubernetes.Clientset, namespace string) ([]metav1.OwnerReference, error) {
	// Use a background context for a single API call.
	ctx := context.Background()

	// Determine the pod name from the hostname.
	podName, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to read hostname: %w", err)
	}

	// Fetch the pod to build the owner reference.
	pod, err := getKuberhealthyPod(ctx, client, namespace, strings.ToLower(podName))
	if err != nil {
		return nil, err
	}

	ownerRef := metav1.OwnerReference{
		APIVersion: podAPIVersion,
		Kind:       podKind,
		Name:       pod.GetName(),
		UID:        pod.GetUID(),
	}

	return []metav1.OwnerReference{ownerRef}, nil
}

// getKuberhealthyPod fetches the current pod spec.
func getKuberhealthyPod(ctx context.Context, client *kubernetes.Clientset, namespace, podName string) (*apiv1.Pod, error) {
	// Load the pod from the API server.
	podClient := client.CoreV1().Pods(namespace)
	podSpec, err := podClient.Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pod %s: %w", podName, err)
	}

	return podSpec, nil
}

// getCurrentUser checks which os user is running the app.
func getCurrentUser(defaultUser int64) (int64, error) {
	// Load the current user from the OS.
	currentUser, err := user.Current()
	if err != nil {
		return 0, err
	}

	// Parse the user ID.
	intCurrentUser, err := strconv.ParseInt(currentUser.Uid, 0, 64)
	if err != nil {
		return 0, err
	}

	// Fall back to the default user when running as root.
	if intCurrentUser == 0 {
		log.Debugln("Running as root. Using default user:", defaultUser)
		return defaultUser, nil
	}

	return intCurrentUser, nil
}
