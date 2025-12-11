package envoy

import (
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/platform-service-gateway/api/gateway/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var (
	testCluster = &clustersv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
		},
	}
)

const (
	chartUrl = "oci://docker.io/envoyproxy/gateway-helm"
	chartTag = "1.5.4"
)

func Test_Gateway_InstallOrUpdate(t *testing.T) {
	testCases := []struct {
		desc                     string
		clusterInterceptorFuncs  interceptor.Funcs
		clusterInitObjs          []client.Object
		platformInterceptorFuncs interceptor.Funcs
		platformInitObjs         []client.Object
	}{
		{
			desc: "",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			clusterClient := fake.NewClientBuilder().
				WithInterceptorFuncs(tC.clusterInterceptorFuncs).
				WithObjects(tC.clusterInitObjs...).
				Build()

			platformClient := fake.NewClientBuilder().
				WithInterceptorFuncs(tC.platformInterceptorFuncs).
				WithObjects(tC.platformInitObjs...).
				Build()

			g := &Gateway{
				PlatformClient: platformClient,
				ClusterClient:  clusterClient,
				Cluster:        testCluster,
				FluxKubeconfig: &meta.KubeConfigReference{
					SecretRef: &meta.SecretKeyReference{
						Name: "secret",
						Key:  "kubeconfig",
					},
				},
				EnvoyConfig: v1alpha1.EnvoyGatewayConfig{
					Chart: v1alpha1.EnvoyGatewayChart{
						URL: chartUrl,
						Tag: chartTag,
					},
				},
			}
		})
	}
}
