package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Globals for revealing daemonsets that fail to be removed or nodes missing pods.
var nodesMissingDSPod []string

// podRemovalList stores pod removal info for error messages.
var podRemovalList *apiv1.PodList

// runCheckAsync runs the daemonset check asynchronously.
func runCheckAsync(ctx context.Context) <-chan error {
	// Create a channel to signal completion.
	doneChan := make(chan error, 1)

	// Run the check in a goroutine.
	go runCheckWorker(ctx, doneChan)

	return doneChan
}

// runCheckWorker executes the daemonset check and reports to the channel.
func runCheckWorker(ctx context.Context, doneChan chan<- error) {
	// Execute the check and send the result.
	doneChan <- runCheck(ctx)
}

// cleanupAsync runs cleanup in the background.
func cleanupAsync(ctx context.Context) <-chan error {
	// Create a channel to signal cleanup completion.
	doneChan := make(chan error, 1)

	// Run cleanup in a goroutine.
	go cleanupWorker(ctx, doneChan)

	return doneChan
}

// cleanupWorker executes cleanup and reports results.
func cleanupWorker(ctx context.Context, doneChan chan<- error) {
	// Run cleanup and send the result.
	doneChan <- cleanUp(ctx)
}

// runCheck runs pre-check cleanup and then the full daemonset check.
func runCheck(ctx context.Context) error {
	// Start the main check flow.
	log.Infoln("Running daemonset check")

	// Execute the daemonset check.
	err := runDaemonsetCheck(ctx)
	if err != nil {
		return err
	}

	return nil
}

// runDaemonsetCheck deploys and removes the daemonset.
func runDaemonsetCheck(ctx context.Context) error {
	// Deploy the daemonset.
	log.Infoln("Running daemonset deploy...")
	err := deploy(ctx)
	if err != nil {
		return err
	}

	// Remove the daemonset and block until completed.
	log.Infoln("Running daemonset removal...")
	err = remove(ctx, daemonSetName)
	if err != nil {
		return err
	}

	return nil
}

// deploy runs doDeploy and checks for any errors during deployment.
func deploy(ctx context.Context) error {
	// Begin daemonset deployment.
	log.Infoln("Deploying daemonset.")

	// Deploy and stop on error.
	err := doDeploy(ctx)
	if err != nil {
		return fmt.Errorf("error deploying daemonset: %w", err)
	}

	// Wait for pods to come online.
	doneChan := waitForPodsToComeOnlineAsync(ctx)

	// Set daemonset deploy deadline.
	deadlineChan := time.After(checkDeadline.Sub(now))

	// Watch for success, timeout, or cancellation.
	select {
	case err = <-doneChan:
		if err != nil {
			return fmt.Errorf("error waiting for pods to come online: %w", err)
		}
		log.Infoln("Successfully deployed daemonset.")
	case <-deadlineChan:
		log.Debugln("nodes missing DS pods:", nodesMissingDSPod)
		return errors.New("Reached check pod timeout: " + checkDeadline.Sub(now).String() + " waiting for all pods to come online. " +
			"Node(s) missing daemonset pod: " + formatNodes(nodesMissingDSPod))
	case <-ctx.Done():
		// Add node details to the interrupt error when they are available.
		missingNodes := formatNodes(nodesMissingDSPod)
		message := "failed to complete check due to an interrupt signal. canceling deploying daemonset and shutting down from interrupt"
		if len(missingNodes) > 0 {
			message = message + ". Node(s) missing daemonset pod: " + missingNodes
		}
		return errors.New(message)
	}

	return nil
}

// waitForPodsToComeOnlineAsync runs waitForPodsToComeOnline in the background.
func waitForPodsToComeOnlineAsync(ctx context.Context) <-chan error {
	// Create a channel to signal completion.
	doneChan := make(chan error, 1)

	// Run the wait in a goroutine.
	go waitForPodsToComeOnlineWorker(ctx, doneChan)

	return doneChan
}

// waitForPodsToComeOnlineWorker waits for pods and reports results.
func waitForPodsToComeOnlineWorker(ctx context.Context, doneChan chan<- error) {
	// Execute wait and send the result.
	doneChan <- waitForPodsToComeOnline(ctx)
}

// doDeploy creates a daemonset.
func doDeploy(ctx context.Context) error {
	// Generate the spec for the daemonset.
	daemonSetSpec := generateDaemonSetSpec(ctx)

	// Create the daemonset.
	err := createDaemonset(ctx, daemonSetSpec)
	return err
}

// remove deletes the daemonset and waits for resources to clear.
func remove(ctx context.Context, dsName string) error {
	// Begin removal.
	log.Infoln("Removing daemonset.")

	// Delete the daemonset in the background.
	doneChan := deleteDSAsync(ctx, dsName)

	// Set daemonset remove deadline.
	deadlineChan := time.After(checkDeadline.Sub(now))

	// Wait for delete command or timeout.
	select {
	case err := <-doneChan:
		if err != nil {
			return fmt.Errorf("error trying to delete daemonset: %w", err)
		}
		log.Infoln("Successfully requested daemonset removal.")
	case <-deadlineChan:
		return errors.New("Reached check pod timeout: " + checkDeadline.Sub(now).String() + " waiting for daemonset removal command to complete.")
	case <-ctx.Done():
		return errors.New("failed to complete check due to shutdown signal. canceling daemonset removal and shutting down from interrupt")
	}

	// Wait for daemonset removal.
	doneChan = waitForDSRemovalAsync(ctx)

	select {
	case err := <-doneChan:
		if err != nil {
			return fmt.Errorf("error waiting for daemonset removal: %w", err)
		}
		log.Infoln("Successfully removed daemonset.")
	case <-deadlineChan:
		return errors.New("Reached check pod timeout: " + checkDeadline.Sub(now).String() + " waiting for daemonset removal.")
	case <-ctx.Done():
		return errors.New("failed to complete check due to an interrupt signal. canceling removing daemonset and shutting down from interrupt")
	}

	// Wait for daemonset pods to be removed.
	doneChan = waitForPodRemovalAsync(ctx)

	select {
	case err := <-doneChan:
		if err != nil {
			return fmt.Errorf("error waiting for daemonset pods removal: %w", err)
		}
		log.Infoln("Successfully removed daemonset pods.")
	case <-deadlineChan:
		unClearedDSPodsNodes := getDSPodsNodeList(podRemovalList)
		return errors.New("reached check pod timeout: " + checkDeadline.Sub(now).String() + " waiting for daemonset pods removal. " + "Node(s) failing to remove daemonset pod: " + unClearedDSPodsNodes)
	case <-ctx.Done():
		return errors.New("failed to complete check due to an interrupt signal. canceling removing daemonset pods and shutting down from interrupt")
	}

	return nil
}

// deleteDSAsync deletes the daemonset asynchronously.
func deleteDSAsync(ctx context.Context, dsName string) <-chan error {
	// Create a channel to signal completion.
	doneChan := make(chan error, 1)

	// Run deletion in a goroutine.
	go deleteDSWorker(ctx, dsName, doneChan)

	return doneChan
}

// deleteDSWorker deletes the daemonset and reports results.
func deleteDSWorker(ctx context.Context, dsName string, doneChan chan<- error) {
	// Run the delete and report the result.
	doneChan <- deleteDS(ctx, dsName)
}

// waitForDSRemovalAsync waits for daemonset removal asynchronously.
func waitForDSRemovalAsync(ctx context.Context) <-chan error {
	// Create a channel to signal completion.
	doneChan := make(chan error, 1)

	// Run the wait in a goroutine.
	go waitForDSRemovalWorker(ctx, doneChan)

	return doneChan
}

// waitForDSRemovalWorker waits for daemonset removal.
func waitForDSRemovalWorker(ctx context.Context, doneChan chan<- error) {
	// Execute wait and send the result.
	doneChan <- waitForDSRemoval(ctx)
}

// waitForPodRemovalAsync waits for pod removal asynchronously.
func waitForPodRemovalAsync(ctx context.Context) <-chan error {
	// Create a channel to signal completion.
	doneChan := make(chan error, 1)

	// Run the wait in a goroutine.
	go waitForPodRemovalWorker(ctx, doneChan)

	return doneChan
}

// waitForPodRemovalWorker waits for pod removal and reports results.
func waitForPodRemovalWorker(ctx context.Context, doneChan chan<- error) {
	// Execute wait and send the result.
	doneChan <- waitForPodRemoval(ctx)
}

// waitForPodsToComeOnline blocks until all pods of the daemonset are deployed and online.
func waitForPodsToComeOnline(ctx context.Context) error {
	// Counter for readiness checks.
	var counter int

	// Log the timeout window.
	log.Infoln("Timeout set:", checkDeadline.Sub(now).String(), "for all daemonset pods to come online")

	for {
		select {
		case <-ctx.Done():
			return errors.New("DaemonsetChecker: Node(s) which were unable to schedule before context was cancelled: " + formatNodes(nodesMissingDSPod))
		default:
		}

		time.Sleep(time.Second)

		// Determine nodes missing pods.
		var err error
		nodesMissingDSPod, err = getNodesMissingDSPod(ctx)
		if err != nil {
			log.Warningln("DaemonsetChecker: Error determining which node was unschedulable. Retrying.", err)
			continue
		}

		// Ensure pods are healthy for a few consecutive seconds.
		readySeconds := 5
		if len(nodesMissingDSPod) <= 0 {
			counter++
			log.Infoln("DaemonsetChecker: All daemonset pods have been ready for", counter, "/", readySeconds, "seconds.")
			if counter >= readySeconds {
				log.Infoln("DaemonsetChecker: Daemonset", daemonSetName, "done deploying pods.")
				return nil
			}
			continue
		}

		if counter > 0 {
			log.Infoln("DaemonsetChecker: Daemonset", daemonSetName, "was ready for", counter, "out of,", readySeconds, "seconds but has left the ready state. Restarting", readySeconds, "second timer.")
			counter = 0
		}

		log.Infoln("DaemonsetChecker: Daemonset check waiting for", len(nodesMissingDSPod), "pod(s) to come up on nodes", nodesMissingDSPod)
	}
}

// waitForDSRemoval waits for the daemonset to be removed before returning.
func waitForDSRemoval(ctx context.Context) error {
	// Poll until the daemonset is removed.
	for {
		select {
		case <-ctx.Done():
			return errors.New("Waiting for daemonset: " + daemonSetName + " removal aborted by context cancellation.")
		default:
		}

		ctxErr := ctx.Err()
		if ctxErr != nil {
			return ctxErr
		}

		time.Sleep(time.Second / 2)
		exists, err := fetchDS(ctx, daemonSetName)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
	}
}

// waitForPodRemoval waits for the daemonset to finish removing all daemonset pods.
func waitForPodRemoval(ctx context.Context) error {
	// Periodically reissue deletes to mitigate race conditions.
	deleteTicker := time.NewTicker(time.Second * 30)
	defer deleteTicker.Stop()

	for {
		var err error
		podRemovalList, err = listPods(ctx)
		if err != nil {
			return errors.New("Failed to list daemonset: " + daemonSetName + " pods: " + err.Error())
		}

		log.Infoln("DaemonsetChecker using LabelSelector: kh-app=" + daemonSetName + ",source=kuberhealthy,khcheck=daemonset to remove ds pods")

		select {
		case <-deleteTicker.C:
			log.Infoln("DaemonsetChecker re-issuing a pod delete command for daemonset checkers.")
			err = deletePods(ctx, daemonSetName)
			if err != nil {
				return errors.New("Failed to delete daemonset " + daemonSetName + " pods: " + err.Error())
			}
		case <-ctx.Done():
			return errors.New("timed out when waiting for pod removal")
		default:
		}

		log.Infoln("DaemonsetChecker waiting for", len(podRemovalList.Items), "pods to delete")
		for _, pod := range podRemovalList.Items {
			log.Infoln("DaemonsetChecker is still removing:", pod.Namespace, pod.Name, "on node", pod.Spec.NodeName)
		}

		if len(podRemovalList.Items) == 0 {
			log.Infoln("DaemonsetChecker has finished removing all daemonset pods")
			return nil
		}
		time.Sleep(time.Second * 1)
	}
}

// generateDaemonSetSpec generates a daemonset spec to deploy into the cluster.
func generateDaemonSetSpec(ctx context.Context) *appsv1.DaemonSet {
	// Build the run time label.
	checkRunTime := strconv.Itoa(int(now.Unix()))
	terminationGracePeriod := int64(1)

	// Resolve runAsUser for the daemonset pod.
	runAsUser := defaultUser
	currentUser, err := getCurrentUser(defaultUser)
	if err != nil {
		log.Errorln("Unable to get the current user id", err)
	}
	log.Debugln("runAsUser will be set to", currentUser)
	runAsUser = currentUser

	// Default to tolerating all taints when none provided.
	if len(tolerations) == 0 {
		var tolErr error
		tolerations, tolErr = findAllUniqueTolerations(ctx, client)
		if tolErr != nil {
			log.Warningln("Unable to generate list of pod scheduling tolerations", tolErr)
		}
	}

	// Build owner references for the daemonset pods.
	ownerRef, err := getOwnerRef(client, checkNamespace)
	if err != nil {
		log.Errorln("Error getting ownerReference:", err)
	}

	// Set node selector to nil when none provided.
	if len(dsNodeSelectors) == 0 {
		dsNodeSelectors = nil
	}

	// Construct the daemonset.
	log.Infoln("Generating daemonset kubernetes spec.")
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: daemonSetName,
			Labels: map[string]string{
				"kh-app":           daemonSetName,
				"source":           "kuberhealthy",
				"khcheck":          "daemonset",
				"creatingInstance": hostName,
				"checkRunTime":     checkRunTime,
			},
			OwnerReferences: ownerRef,
		},
		Spec: appsv1.DaemonSetSpec{
			MinReadySeconds: 2,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kh-app":           daemonSetName,
					"source":           "kuberhealthy",
					"khcheck":          "daemonset",
					"creatingInstance": hostName,
					"checkRunTime":     checkRunTime,
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"kh-app":           daemonSetName,
						"source":           "kuberhealthy",
						"khcheck":          "daemonset",
						"creatingInstance": hostName,
						"checkRunTime":     checkRunTime,
					},
					Name:            daemonSetName,
					OwnerReferences: ownerRef,
					Annotations: map[string]string{
						"cluster-autoscaler.kubernetes.io/safe-to-evict": "true",
					},
				},
				Spec: apiv1.PodSpec{
					TerminationGracePeriodSeconds: &terminationGracePeriod,
					Tolerations:                   []apiv1.Toleration{},
					PriorityClassName:             podPriorityClassName,
					Containers: []apiv1.Container{
						{
							Name:  "sleep",
							Image: dsPauseContainerImage,
							SecurityContext: &apiv1.SecurityContext{
								RunAsUser: &runAsUser,
							},
							Resources: apiv1.ResourceRequirements{
								Requests: apiv1.ResourceList{
									apiv1.ResourceCPU:    resource.MustParse("0"),
									apiv1.ResourceMemory: resource.MustParse("0"),
								},
							},
						},
					},
					NodeSelector: dsNodeSelectors,
				},
			},
		},
	}

	// Add tolerations to the daemonset spec.
	daemonSet.Spec.Template.Spec.Tolerations = append(daemonSet.Spec.Template.Spec.Tolerations, tolerations...)
	log.Infoln("Deploying daemonset with tolerations:", daemonSet.Spec.Template.Spec.Tolerations)

	return daemonSet
}

// findAllUniqueTolerations returns all taints present on nodes as tolerations.
func findAllUniqueTolerations(ctx context.Context, client *kubernetes.Clientset) ([]apiv1.Toleration, error) {
	// Initialize results.
	var uniqueTolerations []apiv1.Toleration

	// Fetch nodes from the cluster.
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return uniqueTolerations, err
	}
	log.Debugln("Searching for unique taints on the cluster.")

	// Track unique taints by value.
	keys := make(map[string]bool)
	for _, node := range nodes.Items {
		for _, taint := range node.Spec.Taints {
			// Skip taints listed in ALLOWED_TAINTS.
			val, exists := allowedTaints[taint.Key]
			if exists {
				if val == taint.Effect {
					continue
				}
			}

			// Add unique taints to the toleration list.
			_, valueExists := keys[taint.Value]
			if !valueExists {
				keys[taint.Value] = true
				uniqueTolerations = append(uniqueTolerations, apiv1.Toleration{Key: taint.Key, Value: taint.Value, Effect: taint.Effect})
			}
		}
	}

	log.Infoln("Found taints to tolerate:", uniqueTolerations)
	return uniqueTolerations, nil
}

// getNodesMissingDSPod gets a list of nodes that do not have a DS pod running on them.
func getNodesMissingDSPod(ctx context.Context) ([]string, error) {
	// Initialize the result list.
	var nodesMissingDSPods []string

	// Fetch all nodes.
	nodes, err := listNodes(ctx)
	if err != nil {
		return nodesMissingDSPods, errors.New("Failed to list nodes: " + err.Error())
	}

	// Fetch daemonset pods.
	pods, err := listPods(ctx)
	if err != nil {
		return nodesMissingDSPods, errors.New("Failed to list daemonset: " + daemonSetName + " pods: " + err.Error())
	}

	// Initialize node status map for schedulable nodes.
	nodeStatuses := make(map[string]bool)
	for _, node := range nodes.Items {
		if taintsAreTolerated(node.Spec.Taints, tolerations) && nodeLabelsMatch(node.Labels, dsNodeSelectors) {
			nodeStatuses[node.Name] = false
		}
	}

	// Mark nodes that have running daemonset pods.
	for _, pod := range pods.Items {
		if pod.Status.Phase != "Running" {
			continue
		}
		for _, node := range nodes.Items {
			for _, nodeIP := range node.Status.Addresses {
				if nodeIP.Type != "InternalIP" || nodeIP.Address != pod.Status.HostIP {
					continue
				}
				if taintsAreTolerated(node.Spec.Taints, tolerations) {
					nodeStatuses[node.Name] = true
					break
				}
			}
		}
	}

	// Collect nodes without daemonset pods.
	for nodeName, hasDS := range nodeStatuses {
		if !hasDS {
			nodesMissingDSPods = append(nodesMissingDSPods, nodeName)
		}
	}

	return nodesMissingDSPods, nil
}

// taintsAreTolerated checks that all taints are tolerated by supplied tolerations.
func taintsAreTolerated(taints []apiv1.Taint, tolerations []apiv1.Toleration) bool {
	for _, taint := range taints {
		var taintIsTolerated bool
		for _, toleration := range tolerations {
			if taint.Key == toleration.Key && taint.Value == toleration.Value {
				taintIsTolerated = true
				break
			}
		}
		if !taintIsTolerated {
			return false
		}
	}

	return true
}

// nodeLabelsMatch checks labels against node selectors.
func nodeLabelsMatch(labels, nodeSelectors map[string]string) bool {
	// Assume selectors match until proven otherwise.
	labelsMatch := true

	for selectorKey, selectorValue := range nodeSelectors {
		labelValue, exists := labels[selectorKey]
		if !exists {
			labelsMatch = false
			continue
		}
		if labelValue != selectorValue {
			labelsMatch = false
		}
	}

	return labelsMatch
}

// deleteDS deletes the specified daemonset and daemonset pods.
func deleteDS(ctx context.Context, dsName string) error {
	// Log and confirm the number of daemonset pods to remove.
	log.Infoln("DaemonsetChecker deleting daemonset:", dsName)

	pods, err := listPods(ctx)
	if err != nil {
		return errors.New("Failed to list daemonset: " + daemonSetName + " pods: " + err.Error())
	}
	log.Infoln("There are", len(pods.Items), "daemonset pods to remove")

	// Delete the daemonset.
	err = deleteDaemonset(ctx, dsName)
	if err != nil {
		return errors.New("Failed to delete daemonset: " + dsName + err.Error())
	}

	// Delete daemonset pods directly.
	log.Infoln("DaemonsetChecker removing daemonset. Proceeding to remove daemonset pods")
	err = deletePods(ctx, dsName)
	if err != nil {
		return errors.New("Failed to delete daemonset " + dsName + " pods: " + err.Error())
	}

	return nil
}

// fetchDS checks if the daemonset exists.
func fetchDS(ctx context.Context, dsName string) (bool, error) {
	// Track pagination state.
	firstQuery := true
	var more string

	for firstQuery || len(more) > 0 {
		firstQuery = false
		dsList, err := listDaemonsets(ctx, more)
		if err != nil {
			log.Errorln(err.Error())
			return false, err
		}
		more = dsList.Continue

		for _, item := range dsList.Items {
			if item.GetName() == dsName {
				return true, nil
			}
		}
	}

	return false, nil
}
