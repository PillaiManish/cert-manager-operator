package istiocsr

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	networkPolicyOwnerLabel = "cert-manager.operator.openshift.io/owned-by"
)

func (r *Reconciler) createOrApplyNetworkPolicies(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	r.log.V(4).Info("reconciling istio-csr network policies", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())

	// Apply static network policy assets for istio-csr
	istioCSRNetworkPolicyAssets := []string{
		"istio-csr/istio-csr-deny-all-networkpolicy.yaml",
		"istio-csr/istio-csr-api-server-egress-networkpolicy.yaml",
		"istio-csr/istio-csr-metrics-ingress-networkpolicy.yaml",
		"istio-csr/istio-csr-grpc-ingress-networkpolicy.yaml",
	}

	for _, assetPath := range istioCSRNetworkPolicyAssets {
		obj := r.getNetworkPolicyFromAsset(assetPath, istiocsr, resourceLabels)
		if err := r.createOrUpdateNetworkPolicy(obj, istioCSRCreateRecon); err != nil {
			return fmt.Errorf("failed to create/update network policy from %s: %w", assetPath, err)
		}
	}

	return nil
}

func (r *Reconciler) getNetworkPolicyFromAsset(assetPath string, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *networkingv1.NetworkPolicy {
	// Get the target namespace for istio-csr deployment
	namespace := istiocsr.GetNamespace()
	if namespace == "" {
		namespace = istiocsr.Spec.IstioCSRConfig.Istio.Namespace
	}

	// Read the asset and decode it
	assetBytes := assets.MustAsset(assetPath)
	obj, err := runtime.Decode(codecs.UniversalDeserializer(), assetBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to decode network policy asset %s: %v", assetPath, err))
	}

	policy := obj.(*networkingv1.NetworkPolicy)

	// Set the correct namespace
	policy.Namespace = namespace

	// Merge resource labels
	if policy.Labels == nil {
		policy.Labels = make(map[string]string)
	}
	for k, v := range resourceLabels {
		policy.Labels[k] = v
	}

	return policy
}

func (r *Reconciler) createIstioCSRDenyAllPolicy(namespace string, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *networkingv1.NetworkPolicy {
	labels := make(map[string]string)
	for k, v := range resourceLabels {
		labels[k] = v
	}
	labels[networkPolicyOwnerLabel] = "istio-csr"

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-csr-deny-all",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "cert-manager-istio-csr",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
}

func (r *Reconciler) createIstioCSRAPIServerEgressPolicy(namespace string, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *networkingv1.NetworkPolicy {
	labels := make(map[string]string)
	for k, v := range resourceLabels {
		labels[k] = v
	}
	labels[networkPolicyOwnerLabel] = "istio-csr"

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-csr-api-server-egress",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "cert-manager-istio-csr",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: &[]corev1.Protocol{corev1.ProtocolTCP}[0],
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 6443},
						},
					},
				},
			},
		},
	}
}

func (r *Reconciler) createIstioCSRMetricsIngressPolicy(namespace string, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *networkingv1.NetworkPolicy {
	labels := make(map[string]string)
	for k, v := range resourceLabels {
		labels[k] = v
	}
	labels[networkPolicyOwnerLabel] = "istio-csr"

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-csr-metrics-ingress",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "cert-manager-istio-csr",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"name": "openshift-monitoring",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: &[]corev1.Protocol{corev1.ProtocolTCP}[0],
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 9402},
						},
					},
				},
			},
		},
	}
}

func (r *Reconciler) createIstioCSRGRPCIngressPolicy(namespace string, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *networkingv1.NetworkPolicy {
	labels := make(map[string]string)
	for k, v := range resourceLabels {
		labels[k] = v
	}
	labels[networkPolicyOwnerLabel] = "istio-csr"

	// Get the gRPC service port from the server config, default to 6443
	grpcPort := int32(6443)
	if istiocsr.Spec.IstioCSRConfig.Server != nil && istiocsr.Spec.IstioCSRConfig.Server.Port != 0 {
		grpcPort = istiocsr.Spec.IstioCSRConfig.Server.Port
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-csr-grpc-ingress",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "cert-manager-istio-csr",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: &[]corev1.Protocol{corev1.ProtocolTCP}[0],
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: grpcPort},
						},
					},
				},
			},
		},
	}
}

func (r *Reconciler) createOrUpdateNetworkPolicy(policy *networkingv1.NetworkPolicy, istioCSRCreateRecon bool) error {
	desired := policy.DeepCopy()
	policyName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling network policy resource", "name", policyName)

	fetched := &networkingv1.NetworkPolicy{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s network policy resource already exists", policyName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(nil, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s network policy resource already exists, maybe from previous installation", policyName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("network policy has been modified, updating to desired state", "name", policyName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s network policy resource", policyName)
		}
		r.eventRecorder.Eventf(nil, corev1.EventTypeNormal, "Reconciled", "network policy resource %s reconciled back to desired state", policyName)
	} else {
		r.log.V(4).Info("network policy resource already exists and is in expected state", "name", policyName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s network policy resource", policyName)
		}
		r.eventRecorder.Eventf(nil, corev1.EventTypeNormal, "Reconciled", "network policy resource %s created", policyName)
	}

	return nil
}
