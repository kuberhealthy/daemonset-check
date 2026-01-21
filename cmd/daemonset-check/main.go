package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kuberhealthy/kuberhealthy/v3/pkg/checkclient"
	nodecheck "github.com/kuberhealthy/kuberhealthy/v3/pkg/nodecheck"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	// Namespace the check daemonset will be created in.
	checkNamespaceEnv = os.Getenv("POD_NAMESPACE")
	checkNamespace    string

	// DSPauseContainerImageOverride specifies the sleep image to use.
	dsPauseContainerImageEnv = os.Getenv("PAUSE_CONTAINER_IMAGE")
	dsPauseContainerImage    string

	// Node selectors for the daemonset check.
	dsNodeSelectorsEnv = os.Getenv("NODE_SELECTOR")
	dsNodeSelectors    = make(map[string]string)

	// Minutes allowed for the shutdown process to complete.
	shutdownGracePeriodEnv = os.Getenv("SHUTDOWN_GRACE_PERIOD")
	shutdownGracePeriod    time.Duration

	// Check daemonset name.
	checkDSNameEnv = os.Getenv("CHECK_DAEMONSET_NAME")
	checkDSName    string

	// The priority class to use for the daemonset.
	podPriorityClassNameEnv = os.Getenv("DAEMONSET_PRIORITY_CLASS_NAME")
	podPriorityClassName    string

	// Check deadline from injected env variable.
	khDeadline    time.Time
	checkDeadline time.Time

	// Daemonset check configurations.
	hostName         string
	tolerationsEnv   = os.Getenv("TOLERATIONS")
	tolerations      []apiv1.Toleration
	daemonSetName    string
	allowedTaintsEnv = os.Getenv("ALLOWED_TAINTS")
	allowedTaints    map[string]apiv1.TaintEffect

	// Time reference for the check.
	now time.Time

	// K8s client used for the check.
	client *kubernetes.Clientset
)

const (
	// Default k8s manifest resource names.
	defaultCheckDSName = "daemonset"
	// Default namespace daemonset check will be performed in.
	defaultCheckNamespace = "kuberhealthy"
	// Default pause container image used for the daemonset check.
	defaultDSPauseContainerImage = "gcr.io/google-containers/pause:3.1"
	// Default shutdown termination grace period.
	defaultShutdownGracePeriod = time.Duration(time.Minute * 1)
	// Default daemonset check deadline.
	defaultCheckDeadline = time.Duration(time.Minute * 15)
	// Default user.
	defaultUser = int64(1000)
	// Default priority class name.
	defaultPodPriorityClassName = ""
)

// init sets up initial state and parses inputs.
func init() {
	// Enable debug output for nodecheck for parity with v2.
	nodecheck.EnableDebugOutput()

	// Capture a timestamp reference for this run.
	now = time.Now()

	// Parse incoming input values and apply defaults.
	parseInputValues()

	// Apply derived configuration values.
	setCheckConfigurations(now)
}

// main wires configuration, executes the daemonset check, and reports status.
func main() {
	// Create a Kubernetes client.
	var err error
	client, err = createKubeClient()
	if err != nil {
		log.Fatalln("Unable to create kubernetes client:", err.Error())
	}
	log.Infoln("Kubernetes client created.")

	// Run node readiness checks.
	err = checksNodeReady()
	if err != nil {
		log.Errorln("Error running when doing the nodechecks:", err)
	}

	// Catch panics and report them.
	defer recoverAndReport()

	// Create a context for our check to operate on.
	log.Debugln("Allowing this check until", checkDeadline, "to finish.")
	log.Debugln("Setting check ctx cancel with timeout", checkDeadline.Sub(now))
	ctx, ctxCancel := context.WithTimeout(context.Background(), checkDeadline.Sub(now))

	// Start listening to interrupts.
	signalChan := make(chan os.Signal, 5)
	go listenForInterrupts(signalChan)

	// Run the check in the background.
	runCheckDoneChan := runCheckAsync(ctx)

	// Wait for either the check to complete or an OS shutdown signal.
	select {
	case err = <-runCheckDoneChan:
		if err != nil {
			reportErrorsToKuberhealthy([]string{"kuberhealthy/daemonset: " + err.Error()})
		} else {
			reportOKToKuberhealthy()
		}
		log.Infoln("Done running daemonset check")
	case <-signalChan:
		reportOKToKuberhealthy()
		log.Errorln("Received shutdown signal. Canceling context and proceeding directly to cleanup.")
		ctxCancel()
	}

	// Run post-check cleanup.
	log.Infoln("Running post-check cleanup")
	shutdownCtx, shutdownCtxCancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer shutdownCtxCancel()

	cleanupDoneChan := cleanupAsync(shutdownCtx)

	select {
	case err = <-cleanupDoneChan:
		if err != nil {
			log.Errorln("Cleanup completed with error:", err)
			return
		}
		log.Infoln("Cleanup completed without error")
	case <-time.After(time.Duration(shutdownGracePeriod)):
		log.Errorln("Shutdown took too long. Shutting down forcefully!")
	}
}

// recoverAndReport reports panics to Kuberhealthy.
func recoverAndReport() {
	// Recover and report panics as failures.
	recovered := recover()
	if recovered == nil {
		return
	}

	log.Infoln("Recovered panic:", recovered)
	message := "kuberhealthy/daemonset: " + stringify(recovered)
	reportErrorsToKuberhealthy([]string{message})
}

// checksNodeReady checks whether node is ready or not before running the check.
func checksNodeReady() error {
	// Create context for readiness checks.
	checkTimeLimit := time.Minute * 1
	nctx, _ := context.WithTimeout(context.Background(), checkTimeLimit)

	// Ensure the Kuberhealthy endpoint is reachable.
	err := nodecheck.WaitForKuberhealthy(nctx)
	if err != nil {
		log.Errorln("Error waiting for kuberhealthy endpoint to be contactable by checker pod with error:", err.Error())
	}

	return nil
}

// setCheckConfigurations sets daemonset configurations.
func setCheckConfigurations(timestamp time.Time) {
	// Build the daemonset name using the host and run time.
	hostName = getHostname()
	daemonSetName = checkDSName + "-" + hostName + "-" + strconv.Itoa(int(timestamp.Unix()))
}

// listenForInterrupts watches for termination signals.
func listenForInterrupts(signalChan chan os.Signal) {
	// Relay incoming OS interrupt signals to the signalChan.
	signal.Notify(signalChan, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)

	// Wait for a signal and exit immediately.
	<-signalChan
	os.Exit(0)
}

// reportErrorsToKuberhealthy reports the specified errors for this check run.
func reportErrorsToKuberhealthy(errs []string) {
	// Log and report errors to Kuberhealthy.
	log.Errorln("Reporting errors to Kuberhealthy:", errs)
	reportToKuberhealthy(false, errs)
}

// reportOKToKuberhealthy reports that there were no errors on this check run to Kuberhealthy.
func reportOKToKuberhealthy() {
	// Log and report success to Kuberhealthy.
	log.Infoln("Reporting success to Kuberhealthy.")
	reportToKuberhealthy(true, []string{})
}

// reportToKuberhealthy reports the check status to Kuberhealthy.
func reportToKuberhealthy(ok bool, errs []string) {
	// Report success or failure depending on ok.
	if ok {
		err := checkclient.ReportSuccess()
		if err != nil {
			log.Fatalln("error reporting to kuberhealthy:", err.Error())
		}
		return
	}

	err := checkclient.ReportFailure(errs)
	if err != nil {
		log.Fatalln("error reporting to kuberhealthy:", err.Error())
	}
}

// stringify converts panic values to strings for logging.
func stringify(value interface{}) string {
	// Handle string values directly.
	text, ok := value.(string)
	if ok {
		return text
	}

	// Handle error values explicitly.
	err, ok := value.(error)
	if ok {
		return err.Error()
	}

	// Fallback to a generic panic message.
	return "unknown panic"
}
