package main

import (
	"errors"
	"strings"
	"time"

	"github.com/kuberhealthy/kuberhealthy/v3/pkg/checkclient"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

// findValEffect splits a toleration segment into value and effect.
func findValEffect(find string) (string, string, error) {
	// Ensure the input is present.
	if len(find) < 1 {
		return "", "", errors.New("empty string in findValEffect")
	}

	// Split value and effect on colon.
	findParts := strings.Split(find, ":")
	if len(findParts) > 1 {
		value := findParts[0]
		effect := findParts[1]
		if len(value) < 1 {
			return "", "", errors.New("empty value after split on :")
		}
		return value, effect, nil
	}

	// Fall back to returning the raw string as the value.
	return find, "", nil
}

// createToleration builds a toleration from a toleration string.
func createToleration(toleration string) (*corev1.Toleration, error) {
	// Validate the input.
	if len(toleration) < 1 {
		return &corev1.Toleration{}, errors.New("must pass toleration value to createToleration")
	}

	// Split key and value.
	splitKV := strings.Split(toleration, "=")
	if len(splitKV) > 1 {
		key := splitKV[0]
		if len(key) < 1 {
			return &corev1.Toleration{}, errors.New("empty key after split on =")
		}

		// Split the value into value/effect when possible.
		value, effect, err := findValEffect(splitKV[1])
		if err != nil {
			log.Errorln(err)
			return &corev1.Toleration{}, err
		}

		// Build the toleration.
		result := corev1.Toleration{
			Key:      key,
			Operator: corev1.TolerationOpEqual,
			Value:    value,
			Effect:   corev1.TaintEffect(effect),
		}
		return &result, nil
	}

	// Build a toleration without a value.
	result := corev1.Toleration{
		Key:      toleration,
		Operator: corev1.TolerationOpExists,
	}
	return &result, nil
}

// createTaintMap adds a taint definition to the supplied map.
func createTaintMap(taintMap map[string]corev1.TaintEffect, taint string) error {
	// Split key/value first.
	splitKV := strings.Split(taint, "=")
	if len(splitKV) > 1 {
		key := splitKV[0]
		if len(key) < 1 {
			return errors.New("empty key after split on =")
		}

		// Split value/effect and record the effect.
		_, effect, err := findValEffect(splitKV[1])
		if err != nil {
			log.Errorln(err)
			return err
		}

		taintMap[key] = corev1.TaintEffect(effect)
		return nil
	}

	// Handle key:effect strings without a value.
	splitKE := strings.Split(taint, ":")
	if len(splitKE[0]) < 1 {
		return errors.New("empty key after split on :")
	}

	key := splitKE[0]
	effect := splitKE[1]
	taintMap[key] = corev1.TaintEffect(effect)
	return nil
}

// parseInputValues parses and sets global vars from env variables and other inputs.
func parseInputValues() {
	// Parse shutdown grace period.
	shutdownGracePeriod = defaultShutdownGracePeriod
	if len(shutdownGracePeriodEnv) != 0 {
		duration, err := time.ParseDuration(shutdownGracePeriodEnv)
		if err != nil {
			log.Fatalln("error occurred attempting to parse SHUTDOWN_GRACE_PERIOD:", err)
		}
		if duration.Minutes() < 1 {
			log.Fatalln("error occurred attempting to parse SHUTDOWN_GRACE_PERIOD. A value less than 1 was parsed:", duration.Minutes())
		}
		shutdownGracePeriod = duration
		log.Infoln("Parsed SHUTDOWN_GRACE_PERIOD:", shutdownGracePeriod)
	}
	log.Infoln("Setting shutdown grace period to:", shutdownGracePeriod)

	// Use injected deadline to set check timeout.
	checkDeadline = now.Add(defaultCheckDeadline)
	deadline, err := checkclient.GetDeadline()
	if err != nil {
		log.Infoln("There was an issue getting the check deadline:", err.Error())
	}
	if err == nil {
		khDeadline = deadline
		checkDeadline = khDeadline.Add(-shutdownGracePeriod)
	}
	log.Infoln("Check deadline in", checkDeadline.Sub(now))

	// Parse namespace.
	checkNamespace = defaultCheckNamespace
	if len(checkNamespaceEnv) != 0 {
		checkNamespace = checkNamespaceEnv
		log.Infoln("Parsed POD_NAMESPACE:", checkNamespace)
	}
	log.Infoln("Performing check in", checkNamespace, "namespace.")

	// Parse pause container image.
	dsPauseContainerImage = defaultDSPauseContainerImage
	if len(dsPauseContainerImageEnv) != 0 {
		log.Infoln("Parsed PAUSE_CONTAINER_IMAGE:", dsPauseContainerImageEnv)
		dsPauseContainerImage = dsPauseContainerImageEnv
	}
	log.Infoln("Setting DS pause container image to:", dsPauseContainerImage)

	// Parse daemonset name.
	checkDSName = defaultCheckDSName
	if len(checkDSNameEnv) != 0 {
		checkDSName = checkDSNameEnv
		log.Infoln("Parsed CHECK_DAEMONSET_NAME:", checkDSName)
	}
	log.Infoln("Setting check daemonset name to:", checkDSName)

	// Parse priority class name.
	podPriorityClassName = defaultPodPriorityClassName
	if len(podPriorityClassNameEnv) != 0 {
		podPriorityClassName = podPriorityClassNameEnv
		log.Infoln("Parsed PRIORITY_CLASS:", podPriorityClassName)
	}
	log.Infoln("Setting check priority class name to:", podPriorityClassName)

	// Parse node selectors.
	if len(dsNodeSelectorsEnv) != 0 {
		splitEnvVars := strings.Split(dsNodeSelectorsEnv, ",")
		for _, pair := range splitEnvVars {
			parsedPair := strings.Split(pair, "=")
			if len(parsedPair) != 2 {
				log.Warnln("Unable to parse key value pair:", pair)
				continue
			}
			_, exists := dsNodeSelectors[parsedPair[0]]
			if !exists {
				dsNodeSelectors[parsedPair[0]] = parsedPair[1]
			}
		}
		log.Infoln("Parsed NODE_SELECTOR:", dsNodeSelectors)
	}

	// Parse tolerations.
	if len(tolerationsEnv) != 0 {
		splitEnvVars := strings.Split(tolerationsEnv, ",")
		if len(splitEnvVars) > 1 {
			for _, toleration := range splitEnvVars {
				parsedTol, err := createToleration(toleration)
				if err != nil {
					log.Errorln(err)
					continue
				}
				tolerations = append(tolerations, *parsedTol)
			}
		}

		parsedTol, err := createToleration(tolerationsEnv)
		if err != nil {
			log.Errorln(err)
			return
		}
		tolerations = append(tolerations, *parsedTol)

		if len(tolerations) > 1 {
			log.Infoln("Parsed TOLERATIONS:", tolerations)
		}
	}

	// Parse allowed taints.
	if len(allowedTaintsEnv) != 0 {
		allowedTaints = make(map[string]corev1.TaintEffect)
		splitEnvVars := strings.Split(allowedTaintsEnv, ",")
		for _, taint := range splitEnvVars {
			err = createTaintMap(allowedTaints, taint)
			if err != nil {
				log.Errorln(err)
				continue
			}
		}

		if len(allowedTaints) != 0 {
			log.Infoln("Parsed ALLOWED_TAINTS:", allowedTaints)
		}
	}
}
