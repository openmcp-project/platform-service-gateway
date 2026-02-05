package envoy

import (
	"fmt"
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/openmcp-project/platform-service-gateway/api/gateway/v1alpha1"
	"github.com/openmcp-project/platform-service-gateway/internal/schemes"
	"github.com/openmcp-project/platform-service-gateway/pkg/utils"
)

type testSetup struct {
	clusterInterceptorFuncs  interceptor.Funcs
	clusterInitObjs          []client.Object
	platformInterceptorFuncs interceptor.Funcs
	platformInitObjs         []client.Object
	imagePullSecrets         []corev1.LocalObjectReference
}

func (ts *testSetup) build() (clusterClient, platformClient client.WithWatch, g *Gateway) {
	clusterClient = fake.NewClientBuilder().
		WithInterceptorFuncs(ts.clusterInterceptorFuncs).
		WithObjects(ts.clusterInitObjs...).
		WithScheme(schemes.Target).
		Build()

	platformClient = fake.NewClientBuilder().
		WithInterceptorFuncs(ts.platformInterceptorFuncs).
		WithObjects(ts.platformInitObjs...).
		WithScheme(schemes.Platform).
		Build()

	g = &Gateway{
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
			Images: &v1alpha1.ImagesConfig{
				ImagePullSecrets: ts.imagePullSecrets,
			},
		},
	}
	return clusterClient, platformClient, g
}

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
		desc string
		testSetup
		expectedErr error
	}{
		{
			desc: "should install without image pull secrets",
		},
		{
			desc: "should install with image pull secrets",
			testSetup: testSetup{
				imagePullSecrets: []corev1.LocalObjectReference{
					{Name: "my-secret"},
				},
				platformInitObjs: []client.Object{
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "my-secret",
							Namespace: testCluster.Namespace,
						},
						Type: corev1.SecretTypeDockerConfigJson,
						Data: map[string][]byte{
							corev1.DockerConfigJsonKey: []byte(`{}`),
						},
					},
				},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			clusterClient, platformClient, g := tC.build()

			err := g.InstallOrUpdate(t.Context())
			if tC.expectedErr != nil {
				assert.ErrorIs(t, err, tC.expectedErr)
				return
			}
			assert.NoError(t, err)

			hr := g.getHelmRelease()
			err = platformClient.Get(t.Context(), client.ObjectKeyFromObject(hr), hr)
			assert.NoError(t, err)

			repo := g.getRepo()
			err = platformClient.Get(t.Context(), client.ObjectKeyFromObject(repo), repo)
			if assert.NoError(t, err) {
				assert.Equal(t, chartUrl, repo.Spec.URL)
				assert.Equal(t, chartTag, repo.Spec.Reference.Tag)
			}

			if tC.imagePullSecrets != nil {
				for _, ps := range tC.imagePullSecrets {
					copied := &corev1.Secret{}
					err := clusterClient.Get(t.Context(), client.ObjectKey{
						Name:      ps.Name,
						Namespace: deploymentNamespace,
					}, copied)
					assert.NoError(t, err)
					assert.NotEmpty(t, copied.Data[corev1.DockerConfigJsonKey])
				}
			}
		})
	}
}

func Test_Gateway_Uninstall(t *testing.T) {
	testCases := []struct {
		desc string
		testSetup
		retries     int
		expectedErr error
	}{
		{
			desc: "should uninstall when objects are already gone",
		},
		{
			desc:    "should uninstall when objects are present",
			retries: 1,
			testSetup: testSetup{
				platformInitObjs: []client.Object{
					&sourcev1.OCIRepository{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("%s.gateway", testCluster.Name),
							Namespace: testCluster.Namespace,
						},
					},
					&helmv2.HelmRelease{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("%s.gateway", testCluster.Name),
							Namespace: testCluster.Namespace,
						},
					},
				},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			_, platformClient, g := tC.build()

			// Run Uninstall and expect it to return
			// a RemainingResourcesError.
			for i := 0; i < tC.retries; i++ {
				err := g.Uninstall(t.Context())
				assert.ErrorIs(t, err, &utils.RemainingResourcesError{})
			}

			// Final run of Uninstall
			err := g.Uninstall(t.Context())
			if tC.expectedErr != nil {
				assert.ErrorIs(t, err, tC.expectedErr)
				return
			}
			assert.NoError(t, err)

			hr := g.getHelmRelease()
			err = platformClient.Get(t.Context(), client.ObjectKeyFromObject(hr), hr)
			assert.True(t, apierrors.IsNotFound(err), "HelmRelease still exists")

			repo := g.getRepo()
			err = platformClient.Get(t.Context(), client.ObjectKeyFromObject(repo), repo)
			assert.True(t, apierrors.IsNotFound(err), "OCIRepository still exists")
		})
	}
}
