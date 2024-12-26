//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/util/wait"
	"net/http"
	"path/filepath"
	"testing"

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

// execToPod runs a command in a pod and returns its stdout and stderr
func execToPod(ctx context.Context, command []string, podName, namespace string, clientset *kubernetes.Clientset, config *rest.Config) (string, string, error) {
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

	for _, cmd := range command {
		req.Param("command", cmd)
	}

	// Set up the executor
	exec, err := remotecommand.NewSPDYExecutor(config, "GET", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("failed to initialize SPDY executor: %w", err)
	}

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", stderr.String(), fmt.Errorf("error streaming command: %w", err)
	}

	return stdout.String(), stderr.String(), nil
}

func TestIstio(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.GetConfig()
	require.NoError(t, err)

	certmanageroperatorclient, err := certmanoperatorclient.NewForConfig(cfg)
	require.NoError(t, err)

	clientset, err := kubernetes.NewForConfig(cfg)
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

	// deploy istio service mesh and check the operator status
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-subscription.yaml"), "openshift-operators")
	err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{}, validOperatorStatusConditions)
	require.NoError(t, err)

	// TODO: to be uncommented
	//loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-csr.yaml"), "istio-csr")
	//defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-csr.yaml"), "istio-csr")

	// deploy istio service mesh and check the operator status
	isitioSystemNS := "istio-system"

	// self-signed issuer and certificate
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), "")
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), "")

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), isitioSystemNS)
	defer loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), isitioSystemNS)

	// applying smcp, service role, issue and certificate for istio
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-issuer.yaml"), isitioSystemNS)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-issuer.yaml"), isitioSystemNS)
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-smcp.yaml"), isitioSystemNS)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-smcp.yaml"), isitioSystemNS)
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-servicerole.yaml"), isitioSystemNS)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-servicerole.yaml"), isitioSystemNS)

	// Label selectors for Istio components
	ingressGatewayLabelSelector := "app=istio-ingressgateway"
	egressGatewayLabelSelector := "app=istio-egressgateway"
	controlPlaneLabelSelector := "app=istiod"

	// Wait for ingress gateway to be running
	err = pollTillRunning(ctx, clientset, isitioSystemNS, ingressGatewayLabelSelector)
	require.NoError(t, err, "ingress gateway pod is not running")

	// Wait for egress gateway to be running
	err = pollTillRunning(ctx, clientset, isitioSystemNS, egressGatewayLabelSelector)
	require.NoError(t, err, "egress gateway pod is not running")

	// Wait for Istio control plane to be running
	err = pollTillRunning(ctx, clientset, isitioSystemNS, controlPlaneLabelSelector)
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
	curlCmd := []string{"curl", "http://httpbin." + testNS.Name + ":8000/ip", "-s", "-o", "/dev/null", "-w", "%{http_code}"}
	podName := podList.Items[0].Name

	stdout, stderr, err := execToPod(ctx, curlCmd, podName, testNS.Name, clientset, cfg)
	require.NoErrorf(t, err, "failed to execute to pod")

	if stdout != "200" {
		t.Errorf("pod execution status is %v not 200 due to err: %v", stdout, stderr)
		t.FailNow()
	}

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "gateway.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "gateway.yaml"), testNS.Name)

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "virtual-service.yaml"), testNS.Name)
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "http-bin", "virtual-service.yaml"), testNS.Name)

	ingressHost, err := getIngressHost(cfg, "istio-system")
	require.NoError(t, err)

	statusCode, err := checkIngressGatewayTraffic(ingressHost)
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
