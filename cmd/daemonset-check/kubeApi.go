package main

import (
	"context"
	"time"

	"github.com/cenkalti/backoff"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

// Use exponential backoff for retries.
var exponentialBackoff = backoff.NewExponentialBackOff()

const (
	// maxElapsedTime bounds backoff retries.
	maxElapsedTime = time.Minute
)

// init configures retry behavior for API calls.
func init() {
	// Apply a max elapsed time for exponential backoff.
	exponentialBackoff.MaxElapsedTime = maxElapsedTime
}

// getDSClient returns a daemonset client.
func getDSClient() appsv1client.DaemonSetInterface {
	// Create a daemonset client scoped to the check namespace.
	log.Debug("Creating Daemonset client.")
	return client.AppsV1().DaemonSets(checkNamespace)
}

// getPodClient returns a pod client.
func getPodClient() corev1client.PodInterface {
	// Create a pod client scoped to the check namespace.
	log.Debug("Creating Pod client.")
	return client.CoreV1().Pods(checkNamespace)
}

// getNodeClient returns a node client.
func getNodeClient() corev1client.NodeInterface {
	// Create a node client for cluster-wide listing.
	log.Debug("Creating Node client.")
	return client.CoreV1().Nodes()
}

// createDaemonset creates the daemonset with retries.
func createDaemonset(ctx context.Context, daemonsetSpec *appsv1.DaemonSet) error {
	// Retry daemonset creation.
	err := backoff.Retry(func() error {
		_, createErr := getDSClient().Create(ctx, daemonsetSpec, metav1.CreateOptions{})
		return createErr
	}, exponentialBackoff)
	if err != nil {
		log.Errorln("Failed to create daemonset. Error:", err)
		return err
	}

	return nil
}

// listDaemonsets lists daemonsets with pagination support.
func listDaemonsets(ctx context.Context, more string) (*appsv1.DaemonSetList, error) {
	// Retry daemonset listing.
	var dsList *appsv1.DaemonSetList
	err := backoff.Retry(func() error {
		var listErr error
		dsList, listErr = getDSClient().List(ctx, metav1.ListOptions{Continue: more})
		return listErr
	}, exponentialBackoff)
	if err != nil {
		log.Errorln("Failed to list daemonsets. Error:", err)
		return dsList, err
	}

	return dsList, nil
}

// deleteDaemonset deletes the daemonset with background propagation.
func deleteDaemonset(ctx context.Context, dsName string) error {
	// Configure background deletion to avoid blocking finalizers.
	policy := metav1.DeletePropagationBackground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &policy}

	// Retry daemonset deletion.
	err := backoff.Retry(func() error {
		deleteErr := getDSClient().Delete(ctx, dsName, deleteOptions)
		return deleteErr
	}, exponentialBackoff)
	if err != nil {
		log.Errorln("Failed to delete daemonset. Error:", err)
		return err
	}

	return nil
}

// listPods lists daemonset pods using the selector.
func listPods(ctx context.Context) (*corev1.PodList, error) {
	// Retry pod listing.
	var podList *corev1.PodList
	err := backoff.Retry(func() error {
		var listErr error
		podList, listErr = getPodClient().List(ctx, metav1.ListOptions{
			LabelSelector: "kh-app=" + daemonSetName + ",source=kuberhealthy,khcheck=daemonset",
		})
		return listErr
	}, exponentialBackoff)
	if err != nil {
		log.Errorln("Failed to list daemonset pods. Error:", err)
		return podList, err
	}

	return podList, nil
}

// deletePods deletes daemonset pods with background propagation.
func deletePods(ctx context.Context, dsName string) error {
	// Configure background deletion to avoid blocking finalizers.
	policy := metav1.DeletePropagationBackground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &policy}

	// Retry pod deletion.
	err := backoff.Retry(func() error {
		deleteErr := getPodClient().DeleteCollection(ctx, deleteOptions, metav1.ListOptions{
			LabelSelector: "kh-app=" + dsName + ",source=kuberhealthy,khcheck=daemonset",
		})
		return deleteErr
	}, exponentialBackoff)
	if err != nil {
		log.Errorln("Failed to delete daemonset pods. Error:", err)
		return err
	}

	return nil
}

// listNodes lists all nodes in the cluster.
func listNodes(ctx context.Context) (*corev1.NodeList, error) {
	// Retry node listing.
	var nodeList *corev1.NodeList
	err := backoff.Retry(func() error {
		var listErr error
		nodeList, listErr = getNodeClient().List(ctx, metav1.ListOptions{})
		return listErr
	}, exponentialBackoff)
	if err != nil {
		log.Errorln("Failed to list nodes. Error:", err)
		return nodeList, err
	}

	return nodeList, nil
}
