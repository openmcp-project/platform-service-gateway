package envoy

import (
	"testing"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func Test_Gateway_Configure(t *testing.T) {
	testCases := []struct {
		desc string
		testSetup
		expectedErr error
	}{
		{
			desc: "should configure gateway when no resources are present",
		},
		{
			desc: "should configure gateway with pull secrets when no resources are present",
			testSetup: testSetup{
				imagePullSecrets: []corev1.LocalObjectReference{
					{Name: "my-secret"},
				},
			},
		},
		{
			desc: "should re-configure gateway when resources are present",
			testSetup: testSetup{
				clusterInitObjs: []client.Object{
					&gatewayv1.GatewayClass{
						ObjectMeta: metav1.ObjectMeta{
							Name: gatewayClassName,
						},
						Spec: gatewayv1.GatewayClassSpec{
							// field is immutable
							ControllerName: gatewayClassControllerName,
						},
					},
					&egv1a1.EnvoyProxy{
						ObjectMeta: metav1.ObjectMeta{
							Name:      gatewayName,
							Namespace: gatewayNamespace,
						},
						Spec: egv1a1.EnvoyProxySpec{
							Provider: &egv1a1.EnvoyProxyProvider{
								Type: egv1a1.EnvoyProxyProviderTypeHost,
							},
						},
					},
					&gatewayv1.Gateway{
						ObjectMeta: metav1.ObjectMeta{
							Name:      gatewayName,
							Namespace: gatewayNamespace,
						},
						Spec: gatewayv1.GatewaySpec{
							GatewayClassName: "bar",
						},
					},
				},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			clusterClient, _, g := tC.build()

			err := g.Configure(t.Context())
			if tC.expectedErr != nil {
				assert.ErrorIs(t, err, tC.expectedErr)
				return
			}
			assert.NoError(t, err)

			gatewayclass := getGatewayClass()
			err = clusterClient.Get(t.Context(), client.ObjectKeyFromObject(gatewayclass), gatewayclass)
			if assert.NoError(t, err) {
				assert.EqualValues(t, gatewayClassControllerName, gatewayclass.Spec.ControllerName)
			}

			envoyProxy := getEnvoyProxy()
			err = clusterClient.Get(t.Context(), client.ObjectKeyFromObject(envoyProxy), envoyProxy)
			if assert.NoError(t, err) {
				assert.Equal(t, egv1a1.EnvoyProxyProviderTypeKubernetes, envoyProxy.Spec.Provider.Type)
				assert.NotNil(t, envoyProxy.Spec.Provider.Kubernetes)

				if tC.imagePullSecrets != nil {
					assert.Len(t, envoyProxy.Spec.Provider.Kubernetes.EnvoyDeployment.Pod.ImagePullSecrets, len(tC.imagePullSecrets))
				}
			}

			gateway := getGateway()
			err = clusterClient.Get(t.Context(), client.ObjectKeyFromObject(gateway), gateway)
			if assert.NoError(t, err) {
				assert.EqualValues(t, gatewayClassName, gateway.Spec.GatewayClassName)
				assert.NotEmpty(t, gateway.Spec.Listeners)
				assert.NotEmpty(t, gateway.Annotations[tlsPortAnnotation])
				assert.NotEmpty(t, gateway.Annotations[baseDomainAnnotation])
			}
		})
	}
}
