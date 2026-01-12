package main

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// cleanUp triggers check cleanup and waits for all rogue daemonsets to clear.
func cleanUp(ctx context.Context) error {
	// Log the cleanup window.
	log.Debugln("Allowing clean up", shutdownGracePeriod, "to finish.")

	// Clean up daemonsets and daemonset pods.
	log.Infoln("Cleaning up daemonsets and daemonset pods")

	daemonSets, err := getAllDaemonsets(ctx)
	if err != nil {
		return err
	}

	// Remove any rogue daemonsets that still exist.
	if len(daemonSets) > 0 {
		for _, ds := range daemonSets {
			log.Infoln("Removing rogue daemonset:", ds.Name)
			err = remove(ctx, ds.Name)
			if err != nil {
				return err
			}
		}
	}

	log.Infoln("Finished cleanup. No rogue daemonsets or daemonset pods exist")
	return nil
}

// getAllDaemonsets fetches all daemonsets created by the daemonset khcheck.
func getAllDaemonsets(ctx context.Context) ([]appsv1.DaemonSet, error) {
	// Prepare a slice to collect daemonsets.
	var allDS []appsv1.DaemonSet
	var cont string

	// Fetch daemonsets created by Kuberhealthy.
	for {
		dsList, err := getDSClient().List(ctx, metav1.ListOptions{
			LabelSelector: "source=kuberhealthy,khcheck=daemonset",
		})
		if err != nil {
			return allDS, fmt.Errorf("error getting all daemonsets: %w", err)
		}
		cont = dsList.Continue

		// Collect the items.
		for _, ds := range dsList.Items {
			allDS = append(allDS, ds)
		}

		// Continue when more items are available.
		if len(cont) == 0 {
			break
		}
	}

	return allDS, nil
}
