package v1alpha1

import (
	"github.com/fluxcd/pkg/apis/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GatewayServiceConfigSpec defines the desired state of GatewayServiceConfig
type GatewayServiceConfigSpec struct {
	// EnvoyGateway configuration.
	EnvoyGateway EnvoyGatewayConfig `json:"envoyGateway"`

	// Clusters that should be included in the gateway configuration.
	Clusters []ClusterTerm `json:"clusters,omitempty"`

	// Gateway configuration.
	Gateway *GatewayConfig `json:"gateway,omitempty"`

	// DNS configuration.
	DNS DNSConfig `json:"dns"`
}

type ClusterTerm struct {
	// Selector for multiple clusters using labels and purpose.
	Selector *ClusterSelector `json:"selector,omitempty"`

	// ClusterRef can be used to reference a single cluster.
	ClusterRef *ClusterRef `json:"clusterRef,omitempty"`
}

type ClusterSelector struct {
	// MatchLabels selects clusters based on labels.
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// MatchPurpose selects clusters based on purpose.
	MatchPurpose string `json:"matchPurpose,omitempty"`
}

type ClusterRef struct {
	// Name of the referenced Cluster.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace of the referenced Cluster.
	// +kubebuilder:default=default
	Namespace string `json:"namespace"`
}

type EnvoyGatewayConfig struct {
	// Images overrides container image locations for Envoy components.
	Images *ImagesConfig `json:"images,omitempty"`

	// Chart configuration for Envoy Gateway.
	Chart EnvoyGatewayChart `json:"chart"`
}

type EnvoyGatewayChart struct {
	// URL to the chart. Default: oci://docker.io/envoyproxy/gateway-helm
	// +kubebuilder:default="oci://docker.io/envoyproxy/gateway-helm"
	URL string `json:"url"`

	// Tag of the chart. Example: 1.5.4
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Tag string `json:"tag"`

	// SecretRef specifies the Secret containing authentication credentials
	// for the OCIRepository.
	// For HTTP/S basic auth the secret must contain 'username' and 'password'
	// fields.
	// Support for TLS auth using the 'certFile' and 'keyFile', and/or 'caFile'
	// keys is deprecated. Please use `.spec.certSecretRef` instead.
	// +optional
	SecretRef *meta.LocalObjectReference `json:"secretRef,omitempty"`
}

type ImagesConfig struct {
	// EnvoyProxy image. Example: docker.io/envoyproxy/envoy:distroless-v1.35.3
	EnvoyProxy string `json:"proxy"`

	// EnvoyGateway image. Example: docker.io/envoyproxy/gateway:v1.5.1
	EnvoyGateway string `json:"gateway"`

	// Ratelimit image. Example: docker.io/envoyproxy/ratelimit:e74a664a
	Ratelimit string `json:"rateLimit"`

	// ImagePullSecrets specifies the Secrets containing authentication credentials
	// for the Envoy Gateway deployment.
	// +optional
	ImagePullSecrets []meta.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

type GatewayConfig struct {
	// TLSPort is the port on which the gateway will listen for TLS traffic.
	// +kubebuilder:default=9443
	TLSPort int32 `json:"tlsPort,omitempty"`
}

type DNSConfig struct {
	// BaseDomain is the domain from which subdomains will be derived. Example: dev.openmcp.example.com.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	BaseDomain string `json:"baseDomain"`

	// SubdomainTemplate defines how subdomains for clusters will be generated.
	// +kubebuilder:default={{.Cluster.Name}}.{{.Cluster.Namespace}}
	// SubdomainTemplate string `json:"subdomainTemplate"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:resource:scope=Cluster

// GatewayServiceConfig is the Schema for the Gateway PlatformService configuration API
type GatewayServiceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GatewayServiceConfigSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayServiceConfigList contains a list of GatewayServiceConfig
type GatewayServiceConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatewayServiceConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GatewayServiceConfig{}, &GatewayServiceConfigList{})
}
