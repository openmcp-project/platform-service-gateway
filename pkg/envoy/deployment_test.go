package envoy

import (
	"context"
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/openmcp-project/platform-service-gateway/api/gateway/v1alpha1"
	"github.com/openmcp-project/platform-service-gateway/internal/schemes"
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
		expectedErr              error
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
				WithScheme(schemes.Target).
				Build()

			platformClient := fake.NewClientBuilder().
				WithInterceptorFuncs(tC.platformInterceptorFuncs).
				WithObjects(tC.platformInitObjs...).
				WithScheme(schemes.Platform).
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
			err := g.InstallOrUpdate(context.Background())
			if tC.expectedErr != nil {
				assert.ErrorIs(t, err, tC.expectedErr)
				return
			}
			assert.NoError(t, err)

			hr := g.getHelmRelease()
			err = platformClient.Get(context.Background(), client.ObjectKeyFromObject(hr), hr)
			assert.NoError(t, err)

			repo := g.getRepo()
			err = platformClient.Get(context.Background(), client.ObjectKeyFromObject(repo), repo)
			if assert.NoError(t, err) {
				assert.Equal(t, chartUrl, repo.Spec.URL)
				assert.Equal(t, chartTag, repo.Spec.Reference.Tag)
			}
		})
	}
}
