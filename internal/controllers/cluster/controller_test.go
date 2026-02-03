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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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
			r := &ClusterReconciler{
				Config: &gatewayv1alpha1.GatewayServiceConfig{
					Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
						Clusters: terms,
					},
				},
			}

			actual := r.shouldReconcile(tC.cluster)
			assert.Equal(t, tC.expected, actual)
		})
	}
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
				eventRecorder:     record.NewFakeRecorder(100),
				ProviderName:      "test",
				ProviderNamespace: "test",
				Config: &gatewayv1alpha1.GatewayServiceConfig{
					Spec: gatewayv1alpha1.GatewayServiceConfigSpec{
						Clusters: terms,
					},
				},
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
