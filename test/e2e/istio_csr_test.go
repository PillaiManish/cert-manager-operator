//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bytes"

	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	"github.com/openshift/cert-manager-operator/test/library"
	routev1 "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func execToPod(ctx context.Context, command []string, podName, namespace string, clientset *kubernetes.Clientset, config *rest.Config) (string, string, map[string]string, int, error) {
	var stdout, stderr string
	var statusCode int
	var lastErr error
	headers := make(map[string]string)

	// Retry logic: up to 3 attempts
	for attempt := 1; attempt <= 3; attempt++ {
		// Create the REST request to exec into the pod
		req := clientset.CoreV1().RESTClient().
			Post().
			Resource("pods").
			Name(podName).
			Namespace(namespace).
			SubResource("exec").
			Param("container", "sleep").
			Param("stdin", "false").
			Param("stdout", "true").
			Param("stderr", "true").
			Param("tty", "false")

		req = req.VersionedParams(&corev1.PodExecOptions{
			Container: "sleep",
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

		// Set up the executor
		exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
		if err != nil {
			lastErr = fmt.Errorf("failed to initialize SPDY executor: %w", err)
			continue
		}

		// Capture stdout and stderr
		var stdoutBuffer, stderrBuffer bytes.Buffer
		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdout: &stdoutBuffer,
			Stderr: &stderrBuffer,
		})
		if err != nil {
			lastErr = fmt.Errorf("error streaming command: %w", err)
			stderr = stderrBuffer.String()
			stdout = stdoutBuffer.String()
			fmt.Printf("Attempt %d failed: %v\n", attempt, lastErr)
		} else {
			// Parse headers from the output
			stdout = stdoutBuffer.String()
			stderr = stderrBuffer.String()

			// Split headers and body
			lines := strings.Split(stdout, "\n")

			if len(lines) > 0 {
				statusLine := lines[0]
				_, err := fmt.Sscanf(statusLine, "HTTP/1.1 %d", &statusCode)
				if err != nil {
					lastErr = fmt.Errorf("failed to parse HTTP status code: %w", err)
					continue
				}
			}

			for _, line := range lines {
				if strings.TrimSpace(line) == "" {
					break
				}
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}

			return stdout, stderr, headers, statusCode, nil
		}

		// Log and wait before retrying
		fmt.Printf("Retrying (%d/3)...\n", attempt)
		time.Sleep(2 * time.Second)
	}

	// After all attempts, return the last error
	return stdout, stderr, headers, statusCode, fmt.Errorf("all attempts failed: %w", lastErr)
}

func TestIstio(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.GetConfig()
	require.NoError(t, err)

	certmanageroperatorclient, err := certmanoperatorclient.NewForConfig(cfg)
	require.NoError(t, err)

	clientset, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)

	dynamicClient, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)

	// check cert-manager operator status
	err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{
		certManagerControllerDeploymentControllerName,
		certManagerWebhookDeploymentControllerName,
		certManagerCAInjectorDeploymentControllerName,
	}, validOperatorStatusConditions)
	require.NoError(t, err)

	loader := library.NewDynamicResourceLoader(ctx, t)

	// create test namespace and deploy test components
	testNS, err := loader.CreateNS("test")
	require.NoErrorf(t, err, "failed to create test namespace")
	defer loader.DeleteTestingNS(testNS.Name, t.Failed)

	//csv check
	// deploy istio service mesh and check the operator status
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-subscription.yaml"), "openshift-operators")
	err = verifySubscriptionAndCSVWithPoller(ctx, "openshift-operators", "servicemeshoperator", dynamicClient)
	require.NoError(t, err)

	// deploy istio service mesh and check the operator status
	isitioSystemNS, err := loader.CreateNS("istio-system")
	require.NoErrorf(t, err, "failed to create test namespace")
	defer loader.DeleteTestingNS(isitioSystemNS.Name, t.Failed)

	// self-signed issuer and certificate
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), "")
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), "")

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), isitioSystemNS.Name)
	defer loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), isitioSystemNS.Name)

	// applying smcp, service role, issue and certificate for istio
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-issuer.yaml"), isitioSystemNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-issuer.yaml"), isitioSystemNS.Name)

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-csr.yaml"), isitioSystemNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-csr.yaml"), isitioSystemNS.Name)

	// TODO: check status of cert-manager-istio-csr
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-smcp.yaml"), isitioSystemNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-smcp.yaml"), isitioSystemNS.Name)
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-servicerole.yaml"), isitioSystemNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-servicerole.yaml"), isitioSystemNS.Name)

	// Label selectors for Istio components
	ingressGatewayLabelSelector := "app=istio-ingressgateway"
	egressGatewayLabelSelector := "app=istio-egressgateway"
	controlPlaneLabelSelector := "app=istiod"

	// Wait for ingress gateway to be running
	err = pollTillRunning(ctx, clientset, "istio-system", ingressGatewayLabelSelector)
	require.NoError(t, err, "ingress gateway pod is not running")

	// Wait for egress gateway to be running
	err = pollTillRunning(ctx, clientset, isitioSystemNS.Name, egressGatewayLabelSelector)
	require.NoError(t, err, "egress gateway pod is not running")

	// Wait for Istio control plane to be running
	err = pollTillRunning(ctx, clientset, isitioSystemNS.Name, controlPlaneLabelSelector)
	require.NoError(t, err, "control plane pod is not running")

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "service-account.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "service-account.yaml"), testNS.Name)
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "service.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "service.yaml"), testNS.Name)
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "deployment.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "deployment.yaml"), testNS.Name)

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "sleep", "service-account.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "sleep", "service-account.yaml"), testNS.Name)
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "sleep", "service.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "sleep", "service.yaml"), testNS.Name)
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "sleep", "deployment.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "sleep", "deployment.yaml"), testNS.Name)

	sleepLabelSelector := "app=sleep"
	httpbinLabelSelector := "app=httpbin"

	// wait till both apps are running
	err = pollTillRunning(ctx, clientset, testNS.Name, sleepLabelSelector)
	require.NoError(t, err, "pod is not running")

	err = pollTillRunning(ctx, clientset, testNS.Name, httpbinLabelSelector)
	require.NoError(t, err, "pod is not running")

	podList, err := clientset.CoreV1().Pods(testNS.Name).List(context.TODO(), metav1.ListOptions{
		LabelSelector: sleepLabelSelector,
	})
	require.NoError(t, err)

	// check if sleep application can access the httpbin service
	//curlCmd := "curl https://httpbin." + testNS.Name + ":8000/status/200 -s -o /dev/null -w %{http_code}"

	curlCmd := []string{"curl", "-i", "-k", "-w '${http_code}'", "http://httpbin." + testNS.Name + ":8000/status/200", "-s"}
	podName := podList.Items[0].Name

	stdout, stderr, headers, statusCode, err := execToPod(ctx, curlCmd, podName, testNS.Name, clientset, cfg)
	require.NoErrorf(t, err, "failed to execute to pod")

	if statusCode != 200 {
		t.Errorf("pod execution status is %v not 200 due to err: %v", stdout, stderr)
		t.FailNow()
	}

	serverName, ok := headers["server"]

	if !ok || serverName != "envoy" {
		t.Errorf("pod execution server name is: %s not envoy", serverName)
		t.FailNow()
	}

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "gateway.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "gateway.yaml"), testNS.Name)

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "virtual-service.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "virtual-service.yaml"), testNS.Name)

	ingressHost, err := getIngressHost(cfg, "istio-system")
	require.NoError(t, err)

	statusCode, err = checkIngressGatewayTraffic(ingressHost)
	require.NoError(t, err)

	if statusCode != 200 {
		t.Errorf("ingress gateway status code is %v should be 200", statusCode)
		t.FailNow()
	}

	t.Logf("test successful")

}

func getIngressHost(config *rest.Config, namespace string) (string, error) {
	routeClient, err := routev1.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create route client: %w", err)
	}

	route, err := routeClient.RouteV1().Routes(namespace).Get(context.TODO(), "istio-ingressgateway", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get ingress gateway route: %w", err)
	}

	return route.Spec.Host, nil
}

func checkIngressGatewayTraffic(ingressHost string) (int, error) {
	url := fmt.Sprintf("http://%s/headers", ingressHost)
	resp, err := http.Head(url)
	if err != nil {
		return 0, fmt.Errorf("failed to send request to ingress gateway: %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}

func pollTillRunning(ctx context.Context, clientset *kubernetes.Clientset, namespace, labelSelector string) error {
	err := wait.PollUntilContextTimeout(ctx, PollInterval, TestTimeout, true, func(ctx context.Context) (bool, error) {
		podList, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return false, err
		}

		if len(podList.Items) == 0 {
			return false, nil
		}

		for _, pod := range podList.Items {
			if pod.Status.Phase != "Running" {
				return false, nil
			}

			// Check each container in the pod
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if !containerStatus.Ready || containerStatus.State.Running == nil {
					return false, nil
				}
			}
		}

		return true, nil
	})

	return err
}

func verifySubscriptionAndCSVWithPoller(ctx context.Context, namespace, subscriptionName string, dynamicClient dynamic.Interface) error {

	// Poll until the CSV is in the `Succeeded` state
	err := wait.PollUntilContextTimeout(ctx, time.Second*1, time.Minute*5, true, func(ctx context.Context) (done bool, err error) {

		// Define the Subscription resource
		subscriptionGVR := schema.GroupVersionResource{
			Group:    "operators.coreos.com",
			Version:  "v1alpha1",
			Resource: "subscriptions",
		}

		// Get the Subscription
		subscription, err := dynamicClient.Resource(subscriptionGVR).Namespace(namespace).Get(ctx, subscriptionName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		// Extract the installed CSV name
		status, found, err := unstructured.NestedMap(subscription.Object, "status")
		if err != nil || !found {
			return false, nil
		}
		installedCSV, found, err := unstructured.NestedString(status, "installedCSV")
		if err != nil || !found || installedCSV == "" {
			return false, nil
		}

		// Define the CSV resource
		csvGVR := schema.GroupVersionResource{
			Group:    "operators.coreos.com",
			Version:  "v1alpha1",
			Resource: "clusterserviceversions",
		}

		// Get the ClusterServiceVersion
		csv, err := dynamicClient.Resource(csvGVR).Namespace(namespace).Get(ctx, installedCSV, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		// Check the CSV status conditions
		status, found, err = unstructured.NestedMap(csv.Object, "status")
		if err != nil || !found {
			return false, nil
		}
		phase, found, err := unstructured.NestedString(status, "phase")

		if phase == "Succeeded" {
			return true, nil
		}

		// Not succeeded yet, continue polling
		return false, nil
	})

	return err
}
