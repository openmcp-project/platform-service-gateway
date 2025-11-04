package cluster

import (
	"testing"

	v1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	gatewayv1alpha1 "github.com/openmcp-project/platform-service-gateway/api/gateway/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

func Test_shouldReconcile(t *testing.T) {
	testCases := []struct {
		desc     string
		cluster  *v1alpha1.Cluster
		expected bool
	}{
		{
			desc: "should reconcile cluster with matching purpose",
			cluster: &v1alpha1.Cluster{
				Spec: v1alpha1.ClusterSpec{
					Purposes: []string{"platform"},
				},
			},
			expected: true,
		},
		{
			desc: "should reconcile cluster with matching labels",
			cluster: &v1alpha1.Cluster{
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
			cluster: &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"webhooks": "true",
					},
				},
				Spec: v1alpha1.ClusterSpec{
					Purposes: []string{"workload"},
				},
			},
			expected: true,
		},
		{
			desc: "should not reconcile cluster with matching labels but wrong purpose",
			cluster: &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"webhooks": "true",
					},
				},
				Spec: v1alpha1.ClusterSpec{
					Purposes: []string{"mcp"},
				},
			},
			expected: false,
		},
		{
			desc: "should not reconcile cluster with matching purpose but wrong labels",
			cluster: &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				Spec: v1alpha1.ClusterSpec{
					Purposes: []string{"workload"},
				},
			},
			expected: false,
		},
		{
			desc: "should reconcile cluster with matching ref",
			cluster: &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
			},
			expected: true,
		},
		{
			desc: "should not reconcile cluster with wrong ref",
			cluster: &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "other",
				},
			},
			expected: false,
		},
		{
			desc: "should reconcile cluster with wrong ref but has finalizer",
			cluster: &v1alpha1.Cluster{
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
