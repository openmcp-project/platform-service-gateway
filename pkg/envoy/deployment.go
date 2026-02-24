package envoy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/platform-service-gateway/api/gateway/v1alpha1"
)

var (
	errFailedToGenerateHelmValuesJSON = errors.New("failed to generate Helm values JSON")
)

const (
	deploymentNamespace = "envoy-gateway-system"
)

type Gateway struct {
	Cluster        *clustersv1alpha1.Cluster
	EnvoyConfig    v1alpha1.EnvoyGatewayConfig
	GatewayConfig  *v1alpha1.GatewayConfig
	DNSConfig      v1alpha1.DNSConfig
	PlatformClient client.Client
	ClusterClient  client.Client
	FluxKubeconfig *fluxmeta.KubeConfigReference
}

func (g *Gateway) InstallOrUpdate(ctx context.Context) error {
	repo := g.getRepo()
	helmRelease := g.getHelmRelease()

	imagePullSecretOps := g.ensureSecrets(ctx, deploymentNamespace)

	ops := make([]applyOperation, 0, 3+len(imagePullSecretOps))
	ops = append(ops, ensureNamespace(deploymentNamespace, g.ClusterClient))
	ops = append(ops, imagePullSecretOps...)
	ops = append(ops,
		applyOperation{
			obj: repo,
			f:   g.reconcileOCIRepositoryFunc(repo),
		},
		applyOperation{
			obj: helmRelease,
			f:   g.reconcileHelmReleaseFunc(repo.Name, helmRelease),
		},
	)

	return createOrUpdate(ctx, g.PlatformClient, ops...)
}

func (g *Gateway) Uninstall(ctx context.Context) error {
	repo := g.getRepo()
	helmRelease := g.getHelmRelease()

	return ensureDeletionOfObjects(ctx, g.PlatformClient, helmRelease, repo)
}

func (g *Gateway) getRepo() *sourcev1.OCIRepository {
	return &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.gateway", g.Cluster.Name),
			Namespace: g.Cluster.Namespace,
		},
	}
}

func (g *Gateway) getHelmRelease() *helmv2.HelmRelease {
	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.gateway", g.Cluster.Name),
			Namespace: g.Cluster.Namespace,
		},
	}
}

func (g *Gateway) reconcileOCIRepositoryFunc(obj *sourcev1.OCIRepository) func() error {
	return func() error {
		obj.Spec.Interval = metav1.Duration{Duration: 10 * time.Hour}
		obj.Spec.LayerSelector = &sourcev1.OCILayerSelector{
			MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			Operation: "copy",
		}
		obj.Spec.URL = g.EnvoyConfig.Chart.URL
		obj.Spec.Reference = &sourcev1.OCIRepositoryRef{
			Tag: g.EnvoyConfig.Chart.Tag,
		}

		obj.Spec.SecretRef = g.EnvoyConfig.Chart.SecretRef

		return nil
	}
}

func (g *Gateway) reconcileHelmReleaseFunc(repoName string, obj *helmv2.HelmRelease) func() error {
	return func() error {
		values, err := g.generateHelmValuesJSON()
		if err != nil {
			return errors.Join(errFailedToGenerateHelmValuesJSON, err)
		}

		obj.Spec.Interval = metav1.Duration{Duration: 1 * time.Hour}
		obj.Spec.Install = &helmv2.Install{
			CRDs: helmv2.CreateReplace,
			Remediation: &helmv2.InstallRemediation{
				Retries: 3,
			},
		}
		obj.Spec.Upgrade = &helmv2.Upgrade{
			CRDs: helmv2.CreateReplace,
			Remediation: &helmv2.UpgradeRemediation{
				Retries: 3,
			},
		}
		obj.Spec.ReleaseName = "eg"
		obj.Spec.StorageNamespace = deploymentNamespace
		obj.Spec.TargetNamespace = deploymentNamespace
		obj.Spec.ChartRef = &helmv2.CrossNamespaceSourceReference{
			Kind: "OCIRepository",
			Name: repoName,
		}
		obj.Spec.Values = values
		obj.Spec.KubeConfig = g.FluxKubeconfig
		return nil
	}
}

func (g *Gateway) reconcileSecretFunc(ctx context.Context, obj *corev1.Secret) func() error {
	return func() error {
		sourceSecret := &corev1.Secret{}
		sourceKey := client.ObjectKey{
			Namespace: g.Cluster.Namespace,
			Name:      obj.Name,
		}
		if err := g.PlatformClient.Get(ctx, sourceKey, sourceSecret); err != nil {
			return fmt.Errorf("failed to get secret %s: %w", sourceKey, err)
		}

		obj.Data = sourceSecret.Data
		obj.Type = sourceSecret.Type
		return nil
	}
}

func (g *Gateway) ensureSecrets(ctx context.Context, targetNamespace string) []applyOperation {
	if g.EnvoyConfig.Images == nil || len(g.EnvoyConfig.Images.ImagePullSecrets) == 0 {
		return nil
	}

	ops := make([]applyOperation, len(g.EnvoyConfig.Images.ImagePullSecrets))
	for i, imagePullSecret := range g.EnvoyConfig.Images.ImagePullSecrets {
		obj := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      imagePullSecret.Name,
				Namespace: targetNamespace,
			},
		}
		ops[i] = applyOperation{
			obj: obj,
			f:   g.reconcileSecretFunc(ctx, obj),
			c:   g.ClusterClient,
		}
	}

	return ops
}

func (g *Gateway) generateHelmValuesJSON() (*apiextensionsv1.JSON, error) {
	values := g.generateHelmValues()
	raw, err := json.Marshal(values)
	return &apiextensionsv1.JSON{Raw: raw}, err
}

func (g *Gateway) generateHelmValues() map[string]any {
	var imagePullSecrets []corev1.LocalObjectReference
	images := map[string]any{}
	if img := g.EnvoyConfig.Images; img != nil {
		imagePullSecrets = img.ImagePullSecrets
		if img.EnvoyGateway != "" {
			images["envoyGateway"] = map[string]any{
				"image": img.EnvoyGateway,
			}
		}
		if img.Ratelimit != "" {
			images["ratelimit"] = map[string]any{
				"image": img.Ratelimit,
			}
		}
	}

	return map[string]any{
		"global": map[string]any{
			"images":           images,
			"imagePullSecrets": imagePullSecrets,
		},
	}
}
