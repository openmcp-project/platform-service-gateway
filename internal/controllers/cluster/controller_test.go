package cluster

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/openmcp-project/controller-utils/pkg/clusters"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	accesslib "github.com/openmcp-project/openmcp-operator/lib/clusteraccess/advanced"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gatewayv1alpha1 "github.com/openmcp-project/platform-service-gateway/api/gateway/v1alpha1"
	"github.com/openmcp-project/platform-service-gateway/internal/schemes"
)

var (
	terms = []gatewayv1alpha1.ClusterTerm{
		{
			Selector: &gatewayv1alpha1.ClusterSelector{
				MatchPurpose: "platform",
			},
		},
		{
			Selector: &gatewayv1alpha1.ClusterSelector{
				MatchLabels: map[string]string{
					"gateway": "true",
				},
			},
		},
		{
			Selector: &gatewayv1alpha1.ClusterSelector{
				MatchLabels: map[string]string{
					"webhooks": "true",
				},
				MatchPurpose: "workload",
			},
		},
		{
			ClusterRef: &gatewayv1alpha1.ClusterRef{
				Name:      "foo",
				Namespace: "bar",
			},
		},
	}

	reqSample = reconcile.Request{NamespacedName: types.NamespacedName{Name: "sample", Namespace: "test"}}
)

func Test_shouldReconcile(t *testing.T) {
	testCases := []struct {
		desc     string
		cluster  *clustersv1alpha1.Cluster
		expected bool
	}{
		{
			desc: "should reconcile cluster with matching purpose",
			cluster: &clustersv1alpha1.Cluster{
				Spec: clustersv1alpha1.ClusterSpec{
					Purposes: []string{"platform"},
				},
			},
			expected: true,
		},
		{
			desc: "should reconcile cluster with matching labels",
			cluster: &clustersv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo":     "bar",
						"gateway": "true",
					},
				},
			},
			expected: true,
		},
		{
			desc: "should reconcile cluster with matching labels and purpose",
			cluster: &clustersv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"webhooks": "true",
					},
				},
				Spec: clustersv1alpha1.ClusterSpec{
					Purposes: []string{"workload"},
				},
			},
			expected: true,
		},
		{
			desc: "should not reconcile cluster with matching labels but wrong purpose",
			cluster: &clustersv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"webhooks": "true",
					},
				},
				Spec: clustersv1alpha1.ClusterSpec{
					Purposes: []string{"mcp"},
				},
			},
			expected: false,
		},
		{
			desc: "should not reconcile cluster with matching purpose but wrong labels",
			cluster: &clustersv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				Spec: clustersv1alpha1.ClusterSpec{
					Purposes: []string{"workload"},
				},
			},
			expected: false,
		},
		{
			desc: "should reconcile cluster with matching ref",
			cluster: &clustersv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
			},
			expected: true,
		},
		{
			desc: "should not reconcile cluster with wrong ref",
			cluster: &clustersv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "other",
				},
			},
			expected: false,
		},
		{
			desc: "should reconcile cluster with wrong ref but has finalizer",
			cluster: &clustersv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "other",
					Finalizers: []string{
						gatewayv1alpha1.GatewayFinalizerOnCluster,
					},
				},
			},
			expected: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			platformClient := fake.NewClientBuilder().
				WithScheme(schemes.Platform).
				WithObjects(
					&gatewayv1alpha1.GatewayServiceConfig{
						ObjectMeta: metav1.ObjectMeta{
							Name: "gateway",
						},
						Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
							Clusters: terms,
						},
					},
				).
				Build()

			r := &ClusterReconciler{
				PlatformCluster: clusters.NewTestClusterFromClient("test", platformClient),
				ProviderName:    "gateway",
			}

			actual := r.shouldReconcile(tC.cluster)
			assert.Equal(t, tC.expected, actual)
		})
	}
}

func Test_isReferencedImagePullSecret(t *testing.T) {
	testCases := []struct {
		desc       string
		cfg        *gatewayv1alpha1.GatewayServiceConfig
		secretName string
		expected   bool
	}{
		{
			desc: "should match referenced ImagePullSecret",
			cfg: &gatewayv1alpha1.GatewayServiceConfig{
				Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
					EnvoyGateway: gatewayv1alpha1.EnvoyGatewayConfig{
						Images: &gatewayv1alpha1.ImagesConfig{
							ImagePullSecrets: []corev1.LocalObjectReference{
								{Name: "my-secret"},
							},
						},
					},
				},
			},
			secretName: "my-secret",
			expected:   true,
		},
		{
			desc: "should not match unrelated secret",
			cfg: &gatewayv1alpha1.GatewayServiceConfig{
				Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
					EnvoyGateway: gatewayv1alpha1.EnvoyGatewayConfig{
						Images: &gatewayv1alpha1.ImagesConfig{
							ImagePullSecrets: []corev1.LocalObjectReference{
								{Name: "my-secret"},
							},
						},
					},
				},
			},
			secretName: "other-secret",
			expected:   false,
		},
		{
			desc: "should not match when no images configured",
			cfg: &gatewayv1alpha1.GatewayServiceConfig{
				Spec: gatewayv1alpha1.GatewayServiceConfigSpec{},
			},
			secretName: "my-secret",
			expected:   false,
		},
		{
			desc: "should not match when no ImagePullSecrets configured",
			cfg: &gatewayv1alpha1.GatewayServiceConfig{
				Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
					EnvoyGateway: gatewayv1alpha1.EnvoyGatewayConfig{
						Images: &gatewayv1alpha1.ImagesConfig{},
					},
				},
			},
			secretName: "my-secret",
			expected:   false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			actual := isReferencedImagePullSecret(tC.cfg, tC.secretName)
			assert.Equal(t, tC.expected, actual)
		})
	}
}

func Test_mapSecretToClusters(t *testing.T) {
	const (
		secretName   = "my-pull-secret"
		clusterNs    = "test-ns"
		providerName = "gateway"
	)

	testCases := []struct {
		desc          string
		secret        *corev1.Secret
		platformObjs  []client.Object
		expectedCount int
	}{
		{
			desc: "should enqueue clusters when referenced ImagePullSecret changes",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: clusterNs,
				},
			},
			platformObjs: []client.Object{
				&gatewayv1alpha1.GatewayServiceConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: providerName,
					},
					Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
						EnvoyGateway: gatewayv1alpha1.EnvoyGatewayConfig{
							Images: &gatewayv1alpha1.ImagesConfig{
								ImagePullSecrets: []corev1.LocalObjectReference{
									{Name: secretName},
								},
							},
						},
						Clusters: []gatewayv1alpha1.ClusterTerm{
							{Selector: &gatewayv1alpha1.ClusterSelector{MatchPurpose: "platform"}},
						},
					},
				},
				&clustersv1alpha1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: clusterNs,
					},
					Spec: clustersv1alpha1.ClusterSpec{
						Purposes: []string{"platform"},
					},
				},
				&clustersv1alpha1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-2",
						Namespace: clusterNs,
					},
					Spec: clustersv1alpha1.ClusterSpec{
						Purposes: []string{"platform"},
					},
				},
			},
			expectedCount: 2,
		},
		{
			desc: "should not enqueue clusters for unrelated secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-secret",
					Namespace: clusterNs,
				},
			},
			platformObjs: []client.Object{
				&gatewayv1alpha1.GatewayServiceConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: providerName,
					},
					Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
						EnvoyGateway: gatewayv1alpha1.EnvoyGatewayConfig{
							Images: &gatewayv1alpha1.ImagesConfig{
								ImagePullSecrets: []corev1.LocalObjectReference{
									{Name: secretName},
								},
							},
						},
						Clusters: []gatewayv1alpha1.ClusterTerm{
							{Selector: &gatewayv1alpha1.ClusterSelector{MatchPurpose: "platform"}},
						},
					},
				},
				&clustersv1alpha1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: clusterNs,
					},
					Spec: clustersv1alpha1.ClusterSpec{
						Purposes: []string{"platform"},
					},
				},
			},
			expectedCount: 0,
		},
		{
			desc: "should not enqueue clusters in different namespace",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "other-ns",
				},
			},
			platformObjs: []client.Object{
				&gatewayv1alpha1.GatewayServiceConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: providerName,
					},
					Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
						EnvoyGateway: gatewayv1alpha1.EnvoyGatewayConfig{
							Images: &gatewayv1alpha1.ImagesConfig{
								ImagePullSecrets: []corev1.LocalObjectReference{
									{Name: secretName},
								},
							},
						},
						Clusters: []gatewayv1alpha1.ClusterTerm{
							{Selector: &gatewayv1alpha1.ClusterSelector{MatchPurpose: "platform"}},
						},
					},
				},
				&clustersv1alpha1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: clusterNs,
					},
					Spec: clustersv1alpha1.ClusterSpec{
						Purposes: []string{"platform"},
					},
				},
			},
			expectedCount: 0,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			platformClient := fake.NewClientBuilder().
				WithScheme(schemes.Platform).
				WithObjects(tC.platformObjs...).
				Build()

			r := &ClusterReconciler{
				PlatformCluster: clusters.NewTestClusterFromClient("platform", platformClient),
				ProviderName:    providerName,
			}

			ctx := context.Background()
			requests := mapSecretToClusterRequests(ctx, r, tC.secret)
			assert.Len(t, requests, tC.expectedCount)
		})
	}
}

// mapSecretToClusterRequests is a test helper that replicates the logic of mapSecretToClusters
// to verify the mapping without needing to unwrap the handler.
func mapSecretToClusterRequests(ctx context.Context, r *ClusterReconciler, secret *corev1.Secret) []reconcile.Request {
	cfg, err := r.getGatewayServiceConfig(ctx, r.ProviderName)
	if err != nil {
		return nil
	}

	if !isReferencedImagePullSecret(cfg, secret.Name) {
		return nil
	}

	clusterList := &clustersv1alpha1.ClusterList{}
	if err := r.PlatformCluster.Client().List(ctx, clusterList, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, cluster := range clusterList.Items {
		if r.shouldReconcile(&cluster) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
			})
		}
	}
	return requests
}

func Test_ClusterReconciler_Reconcile(t *testing.T) {
	testCases := []struct {
		desc                     string
		clusterInterceptorFuncs  interceptor.Funcs
		clusterInitObjs          []client.Object
		platformInterceptorFuncs interceptor.Funcs
		platformInitObjs         []client.Object
		req                      reconcile.Request
		expectedResult           controllerruntime.Result
		expectedErr              error
	}{
		{
			desc:        "should not return error when object does not exist",
			req:         reqSample,
			expectedErr: nil,
		},
		{
			desc: "should not return error when object exist",
			req:  reqSample,
			platformInitObjs: []client.Object{
				&gatewayv1alpha1.GatewayServiceConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gateway",
					},
					Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
						Clusters: terms,
					},
				},
				&clustersv1alpha1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      reqSample.Name,
						Namespace: reqSample.Namespace,
					},
				},
			},
			expectedErr: nil,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			clusterClient := fake.NewClientBuilder().
				WithInterceptorFuncs(tC.clusterInterceptorFuncs).
				WithObjects(tC.clusterInitObjs...).
				WithScheme(schemes.Target).
				Build()

			platformClient := fake.NewClientBuilder().
				WithInterceptorFuncs(tC.platformInterceptorFuncs).
				WithObjects(tC.platformInitObjs...).
				WithScheme(schemes.Platform).
				Build()

			cr := &ClusterReconciler{
				PlatformCluster:   clusters.NewTestClusterFromClient("platform", platformClient),
				eventRecorder:     events.NewFakeRecorder(100),
				ProviderName:      "gateway",
				ProviderNamespace: "test",
				ClusterAccessReconciler: accesslib.NewClusterAccessReconciler(platformClient, ControllerName).
					WithFakeClientGenerator(func(ctx context.Context, kcfgData []byte, scheme *runtime.Scheme, additionalData ...any) (client.Client, error) {
						return clusterClient, nil
					}).Register(accesslib.ExistingCluster("test", "", accesslib.IdentityReferenceGenerator).
					WithTokenAccess(&clustersv1alpha1.TokenConfig{
						RoleRefs: []commonapi.RoleRef{
							{
								Kind: "ClusterRole",
								Name: "cluster-admin",
							},
						},
					}).
					WithNamespaceGenerator(accesslib.RequestNamespaceGenerator).
					WithScheme(schemes.Target).
					Build()),
			}

			ctx := logr.NewContext(t.Context(), logr.New(nil))
			res, err := cr.Reconcile(ctx, tC.req)
			assert.Equal(t, tC.expectedResult, res)
			if tC.expectedErr != nil {
				assert.ErrorIs(t, err, tC.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
