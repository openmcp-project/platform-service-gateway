package envoy

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/openmcp-project/platform-service-gateway/pkg/utils"
)

var (
	errFailedToDeleteObject = errors.New("failed to delete object")
)

const (
	gatewayClassName     = "envoy-gateway"
	gatewayName          = "default"
	gatewayNamespace     = "openmcp-system"
	baseDomainAnnotation = "dns.openmcp.cloud/base-domain"
)

func (g *Gateway) Configure(ctx context.Context) error {
	gatewayclass := getGatewayClass()
	envoyProxy := getEnvoyProxy()
	gateway := getGateway()

	ops := []applyOperation{
		ensureNamespace(gatewayNamespace, nil),
		{
			obj: gatewayclass,
			f:   reconcileGatewayClassFunc(gatewayclass),
		},
		{
			obj: envoyProxy,
			f:   g.reconcileEnvoyProxyFunc(envoyProxy),
		},
		{
			obj: gateway,
			f:   g.reconcileGatewayFunc(gateway),
		},
	}

	err := createOrUpdate(ctx, g.ClusterClient, ops...)
	if utils.IsCRDNotFoundError(err) {
		return utils.NewRetryableError(err, 10*time.Second)
	}
	return err
}

func (g *Gateway) Cleanup(ctx context.Context) error {
	gateway := getGateway()
	envoyProxy := getEnvoyProxy()
	gatewayclass := getGatewayClass()

	return ensureDeletionOfObjects(ctx, g.ClusterClient,
		gateway,
		envoyProxy,
		gatewayclass,
	)
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
		if obj.Spec.Infrastructure == nil {
			obj.Spec.Infrastructure = &gatewayv1.GatewayInfrastructure{}
		}
		obj.Spec.Infrastructure.ParametersRef = &gatewayv1.LocalParametersReference{
			Group: "gateway.envoyproxy.io",
			Kind:  "EnvoyProxy",
			Name:  gatewayName,
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
							"imagePullSecrets": g.EnvoyConfig.Images.ImagePullSecrets,
						},
					},
				},
			},
		}
		return nil
	}
}

// ----- Namespace -----

func ensureNamespace(namespace string, c client.Client) applyOperation {
	return applyOperation{
		obj: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		},
		c: c,
	}
}

// ----- Utils -----

// ensureDeletionOfObjects tries to delete the given objects. It returns a *RetryableError as long as any of the objects still exists.
// The function should be called with the same parameters until it returns nil.
// Any unexpected errors are returned as is.
func ensureDeletionOfObjects(ctx context.Context, c client.Client, objs ...client.Object) error {
	remaining := []client.Object{}
	for _, obj := range objs {
		err := c.Delete(ctx, obj)
		if apierrors.IsNotFound(err) || utils.IsCRDNotFoundError(err) {
			// object not found or CRD does not exist (anymore)
			continue
		}
		if err != nil {
			return errors.Join(errFailedToDeleteObject, err)
		}
		// object may still exist
		remaining = append(remaining, obj)
	}

	if len(remaining) > 0 {
		return utils.NewRemainingResourcesError(10*time.Second, objs...)
	}

	// all objects have been deleted
	return nil
}

type applyOperation struct {
	// obj is the object to be created or updated.
	// Parameters other than name and namespace must be set using the mutate function.
	obj client.Object

	// f is a function which mutates the existing object into its desired state.
	f controllerutil.MutateFn

	// c is an optional parameter to override the client used for this operation.
	c client.Client
}

// createOrUpdate attempts to fetch the given objects from the Kubernetes cluster.
// If an object didn't exist, MutateFn will be called, and it will be created.
// If an object did exist, MutateFn will be called, and if it changed the
// object, it will be updated.
// Otherwise, it will be left unchanged.
func createOrUpdate(ctx context.Context, c client.Client, ops ...applyOperation) error {
	for _, op := range ops {
		opC := c
		if op.c != nil {
			opC = op.c
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, opC, op.obj, op.f); err != nil {
			return err
		}
	}
	return nil
}
