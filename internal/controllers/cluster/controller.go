package cluster

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
	accesslib "github.com/openmcp-project/openmcp-operator/lib/clusteraccess/advanced"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gatewayv1alpha1 "github.com/openmcp-project/platform-service-gateway/api/gateway/v1alpha1"
	"github.com/openmcp-project/platform-service-gateway/pkg/envoy"
)

var (
	errFailedToGetCluster                = errors.New("failed to get Cluster resource")
	errFailedToRemoveOperationAnnotation = errors.New("failed to remove operation annotation")
	errFailedToBuildGatewayManager       = errors.New("failed to build Gateway manager")
	errFailedToGetAccessRequest          = errors.New("failed to get AccessRequest resource")
	errFailedToGetClusterAccess          = errors.New("failed to get access to cluster")
)

const (
	clusterId                      = "cluster"
	requeueAfterRemainingResources = 10 * time.Second

	ControllerName = "GatewayCluster"
)

type ClusterReconciler struct {
	PlatformCluster         *clusters.Cluster
	Config                  *gatewayv1alpha1.GatewayServiceConfig
	eventRecorder           record.EventRecorder
	ProviderName            string
	ProviderNamespace       string
	ClusterAccessReconciler accesslib.ClusterAccessReconciler
}

func NewClusterReconciler(platformCluster *clusters.Cluster, recorder record.EventRecorder, cfg *gatewayv1alpha1.GatewayServiceConfig, providerName, providerNamespace string) *ClusterReconciler {
	return &ClusterReconciler{
		PlatformCluster:   platformCluster,
		eventRecorder:     recorder,
		ProviderName:      providerName,
		ProviderNamespace: providerNamespace,
		Config:            cfg,
		ClusterAccessReconciler: accesslib.NewClusterAccessReconciler(platformCluster.Client(), ControllerName).
			WithManagedLabels(func(controllerName string, req reconcile.Request, _ accesslib.ClusterRegistration) (string, string, map[string]string) {
				return fmt.Sprintf("%s.%s", providerName, controllerName), req.Name, nil
			}).
			Register(accesslib.ExistingCluster(clusterId, "", accesslib.IdentityReferenceGenerator).
				WithTokenAccess(&clustersv1alpha1.TokenConfig{
					RoleRefs: []commonapi.RoleRef{
						{
							Kind: "ClusterRole",
							Name: "cluster-admin",
						},
					},
				}).
				WithNamespaceGenerator(accesslib.RequestNamespaceGenerator).
				Build(),
			),
	}
}

var _ reconcile.Reconciler = &ClusterReconciler{}

func (r *ClusterReconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")

	// no status update, because the Cluster resource doesn't have status fields for Gateway configuration
	// instead, output events for significant changes
	// TODO

	return r.reconcile(ctx, req)
}

func (r *ClusterReconciler) reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx)

	// get Cluster resource
	c := &clustersv1alpha1.Cluster{}
	if err := r.PlatformCluster.Client().Get(ctx, req.NamespacedName, c); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Resource not found")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.Join(errFailedToGetCluster, err)
	}

	// handle operation annotation
	if c.GetAnnotations() != nil {
		op, ok := c.GetAnnotations()[gatewayv1alpha1.OperationAnnotation]
		if !ok {
			// only evaluate the generic operation annotation if no gateway-specific one is set
			op, ok = c.GetAnnotations()[openmcpconst.OperationAnnotation]
		}
		if ok {
			switch op {
			case openmcpconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return ctrl.Result{}, nil
			case openmcpconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, r.PlatformCluster.Client(), c, openmcpconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					return ctrl.Result{}, errors.Join(errFailedToRemoveOperationAnnotation, err)
				}
			}
		}
	}

	if !r.shouldReconcile(c) {
		log.Debug("Ignoring cluster. Does not have a gateway finalizer or a config entry that matches")
		return ctrl.Result{}, nil
	}

	gwMgr, res, err := r.buildGatewayManager(ctx, req, c)
	if err != nil {
		return ctrl.Result{}, errors.Join(errFailedToBuildGatewayManager, err)
	}
	if res.RequeueAfter > 0 {
		return res, nil
	}

	if !c.DeletionTimestamp.IsZero() || !r.enabledForCluster(c) {
		// delete gateway resources
		if err := gwMgr.Cleanup(ctx); err != nil {
			if errors.Is(err, envoy.ErrRemainingResources) {
				return ctrl.Result{RequeueAfter: requeueAfterRemainingResources}, err
			}
			return ctrl.Result{}, err
		}

		// uninstall gateway
		if err := gwMgr.Uninstall(ctx); err != nil {
			if errors.Is(err, envoy.ErrRemainingResources) {
				return ctrl.Result{RequeueAfter: requeueAfterRemainingResources}, err
			}
			return ctrl.Result{}, err
		}

		// TODO: Remove finalizer
		return ctrl.Result{}, nil
	}

	if controllerutil.AddFinalizer(c, gatewayv1alpha1.GatewayFinalizerOnCluster) {
		if err := r.PlatformCluster.Client().Update(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := gwMgr.InstallOrUpdate(ctx); err != nil {
		return ctrl.Result{}, err
	}
	if err := gwMgr.Configure(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// TODO: Publish event
	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clustersv1alpha1.Cluster{}).
		Complete(r)
}

func (r *ClusterReconciler) buildGatewayManager(ctx context.Context, req reconcile.Request, c *clustersv1alpha1.Cluster) (*envoy.Gateway, ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx)
	log.Info("Creating or updating AccessRequest to get access to Cluster")

	res, err := r.ClusterAccessReconciler.Reconcile(ctx, req)
	if err != nil {
		return nil, ctrl.Result{}, err
	}
	if res.RequeueAfter > 0 {
		log.Info("Requeuing because cluster access is not yet available", "after", res.RequeueAfter)
		return nil, res, nil
	}

	ar, err := r.ClusterAccessReconciler.AccessRequest(ctx, req, clusterId)
	if err != nil {
		return nil, ctrl.Result{}, errors.Join(errFailedToGetAccessRequest, err)
	}

	access, err := r.ClusterAccessReconciler.Access(ctx, req, clusterId)
	if err != nil {
		return nil, ctrl.Result{}, errors.Join(errFailedToGetClusterAccess, err)
	}

	clusterClient := access.Client()
	utilruntime.Must(gatewayv1.Install(clusterClient.Scheme()))

	gw := &envoy.Gateway{
		Cluster:        c,
		EnvoyConfig:    r.Config.Spec.EnvoyGateway,
		DNSConfig:      r.Config.Spec.DNS,
		PlatformClient: r.PlatformCluster.Client(),
		ClusterClient:  access.Client(),
		PullSecrets:    []corev1.LocalObjectReference{}, // TODO
		FluxKubeconfig: &fluxmeta.KubeConfigReference{
			SecretRef: &fluxmeta.SecretKeyReference{
				Name: ar.Status.SecretRef.Name,
				Key:  clustersv1alpha1.SecretKeyKubeconfig,
			},
		},
	}
	return gw, ctrl.Result{}, nil
}

func (r *ClusterReconciler) shouldReconcile(cluster *clustersv1alpha1.Cluster) bool {
	return controllerutil.ContainsFinalizer(cluster, gatewayv1alpha1.GatewayFinalizerOnCluster) || r.enabledForCluster(cluster)
}

func (r *ClusterReconciler) enabledForCluster(cluster *clustersv1alpha1.Cluster) bool {
	for _, ct := range r.Config.Spec.Clusters {
		if ct.ClusterRef != nil && refMatches(*ct.ClusterRef, cluster) {
			return true
		}
		if ct.Selector != nil && selectorMatches(*ct.Selector, cluster) {
			return true
		}
	}
	return false
}

func refMatches(ref gatewayv1alpha1.ClusterRef, cluster *clustersv1alpha1.Cluster) bool {
	a := normalizedName(ref.Name, ref.Namespace)
	b := normalizedName(cluster.Name, cluster.Namespace)
	return a == b
}

func normalizedName(name, namespace string) types.NamespacedName {
	if namespace == "" {
		namespace = corev1.NamespaceDefault
	}
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func selectorMatches(sel gatewayv1alpha1.ClusterSelector, cluster *clustersv1alpha1.Cluster) bool {
	return purposeMatches(sel.MatchPurpose, cluster) && labelsMatch(sel.MatchLabels, cluster)
}

func purposeMatches(purpose string, cluster *clustersv1alpha1.Cluster) bool {
	if purpose == "" {
		return true
	}
	return slices.Contains(cluster.Spec.Purposes, purpose)
}

func labelsMatch(labels map[string]string, cluster *clustersv1alpha1.Cluster) bool {
	for label, value := range labels {
		if cluster.Labels[label] != value {
			return false
		}
	}
	return true
}
