package main

import (
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
)

// getHostname attempts to determine the hostname this program is running on.
func getHostname() string {
	// Provide a fallback hostname when unavailable.
	defaultHostname := "kuberhealthy"
	host, err := os.Hostname()
	if len(host) == 0 || err != nil {
		log.Warningln("Unable to determine hostname! Using default placeholder:", defaultHostname)
		return defaultHostname
	}

	return strings.ToLower(host)
}

// formatNodes formats a list into a readable string.
func formatNodes(nodeList []string) string {
	// Join nodes for logging when present.
	if len(nodeList) > 0 {
		return strings.Join(nodeList, ", ")
	}

	return ""
}

// getDSPodsNodeList transforms a pod list into a list of node names.
func getDSPodsNodeList(podList *apiv1.PodList) string {
	// Collect node names for error messages.
	var nodeList []string
	if len(podList.Items) != 0 {
		for _, pod := range podList.Items {
			nodeList = append(nodeList, pod.Spec.NodeName)
		}
	}

	return formatNodes(nodeList)
}
