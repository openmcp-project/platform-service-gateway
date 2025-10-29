package envoy

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	errFailedToCreateGatewayNamespace = errors.New("failed to create Gateway namespace")
	errFailedToApplyGatewayClass      = errors.New("failed to apply GatewayClass")
	errFailedToApplyEnvoyProxy        = errors.New("failed to apply EnvoyProxy")
	errFailedToApplyGateway           = errors.New("failed to apply Gateway")
)

const (
	gatewayClassName     = "envoy-gateway"
	gatewayName          = "default"
	gatewayNamespace     = "openmcp-system"
	baseDomainAnnotation = "dns.openmcp.cloud/base-domain"
)

func (g *Gateway) Configure(ctx context.Context) error {
	gatewayclass := getGatewayClass()
	gatewayclassFunc := reconcileGatewayClassFunc(gatewayclass)
	if _, err := controllerutil.CreateOrUpdate(ctx, g.ClusterClient, gatewayclass, gatewayclassFunc); err != nil {
		return errors.Join(errFailedToApplyGatewayClass, err)
	}

	if err := ensureNamespace(ctx, g.ClusterClient, gatewayNamespace); err != nil {
		return errors.Join(errFailedToCreateGatewayNamespace, err)
	}

	envoyProxy := getEnvoyProxy()
	envoyProxyFunc := g.reconcileEnvoyProxyFunc(envoyProxy)
	if _, err := controllerutil.CreateOrUpdate(ctx, g.ClusterClient, envoyProxy, envoyProxyFunc); err != nil {
		return errors.Join(errFailedToApplyEnvoyProxy, err)
	}

	gateway := getGateway()
	gatewayFunc := g.reconcileGatewayFunc(gateway)
	if _, err := controllerutil.CreateOrUpdate(ctx, g.ClusterClient, gateway, gatewayFunc); err != nil {
		return errors.Join(errFailedToApplyGateway, err)
	}

	return nil
}

func (g *Gateway) Cleanup(ctx context.Context) error {
	// TODO: 1. Delete Gateway, wait for 404
	// TODO: 2. Delete EnvoyProxy resource
	// TODO: 3. Delete gatewayclass

	return nil
}

// ----- GatewayClass -----

func getGatewayClass() *gatewayv1.GatewayClass {
	return &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
		},
	}
}

func reconcileGatewayClassFunc(obj *gatewayv1.GatewayClass) func() error {
	obj.Spec.ControllerName = "gateway.envoyproxy.io/gatewayclass-controller"
	return nil
}

// ----- Gateway -----

func getGateway() *gatewayv1.Gateway {
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: gatewayNamespace,
		},
	}
}

func (g *Gateway) reconcileGatewayFunc(obj *gatewayv1.Gateway) func() error {
	return func() error {
		obj.Spec.GatewayClassName = gatewayClassName
		obj.Spec.Listeners = []gatewayv1.Listener{
			{
				Name:     "tls",
				Port:     9443,
				Protocol: gatewayv1.TLSProtocolType,
				TLS: &gatewayv1.ListenerTLSConfig{
					Mode: ptr.To(gatewayv1.TLSModePassthrough),
				},
				AllowedRoutes: &gatewayv1.AllowedRoutes{
					Namespaces: &gatewayv1.RouteNamespaces{
						From: ptr.To(gatewayv1.NamespacesFromAll),
					},
				},
			},
		}
		obj.Spec.Infrastructure = &gatewayv1.GatewayInfrastructure{
			ParametersRef: &gatewayv1.LocalParametersReference{
				Group: "gateway.envoyproxy.io",
				Kind:  "EnvoyProxy",
				Name:  gatewayName,
			},
		}

		baseDomain := g.generateBaseDomain()
		metav1.SetMetaDataAnnotation(&obj.ObjectMeta, baseDomainAnnotation, baseDomain)

		return nil
	}
}

func (g *Gateway) generateBaseDomain() string {
	return fmt.Sprintf("%s.%s.%s", g.Cluster.Name, g.Cluster.Namespace, g.DNSConfig.BaseDomain)
}

// ----- EnvoyProxy -----

func getEnvoyProxy() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.envoyproxy.io",
		Version: "v1alpha1",
		Kind:    "EnvoyProxy",
	})
	obj.SetName(gatewayName)
	obj.SetNamespace(gatewayNamespace)
	return obj
}

func (g *Gateway) reconcileEnvoyProxyFunc(obj *unstructured.Unstructured) func() error {
	return func() error {
		var container map[string]any
		if g.EnvoyConfig.Images != nil && g.EnvoyConfig.Images.EnvoyProxy != "" {
			container = map[string]any{
				"image": g.EnvoyConfig.Images.EnvoyProxy,
			}
		}

		obj.Object["spec"] = map[string]any{
			"provider": map[string]any{
				"type": "Kubernetes",
				"kubernetes": map[string]any{
					"envoyDeployment": map[string]any{
						"container": container,
						"pod": map[string]any{
							"imagePullSecrets": g.PullSecrets,
						},
					},
				},
			},
		}
		return nil
	}
}

// ----- Namespace -----

func ensureNamespace(ctx context.Context, c client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	return client.IgnoreAlreadyExists(c.Create(ctx, ns))
}
