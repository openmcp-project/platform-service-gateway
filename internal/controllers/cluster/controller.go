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
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gatewayv1alpha1 "github.com/openmcp-project/platform-service-gateway/api/gateway/v1alpha1"
	"github.com/openmcp-project/platform-service-gateway/pkg/envoy"
	"github.com/openmcp-project/platform-service-gateway/pkg/utils"
)

var (
	errFailedToGetCluster                = errors.New("failed to get Cluster resource")
	errFailedToRemoveOperationAnnotation = errors.New("failed to remove operation annotation")
	errFailedToBuildGatewayManager       = errors.New("failed to build Gateway manager")
	errFailedToGetAccessRequest          = errors.New("failed to get AccessRequest resource")
	errFailedToGetClusterAccess          = errors.New("failed to get access to cluster")
	errClusterAccessNotYetAvailable      = errors.New("cluster access is not yet available")
)

const (
	reasonRemainingResources = "RemainingResources"
	reasonGatewayInstalled   = "GatewayInstalled"
	reasonGatewayUninstalled = "GatewayUninstalled"

	actionInstallGateway   = "InstallGateway"
	actionUninstallGateway = "UninstallGateway"

	clusterId = "cluster"

	ControllerName = "GatewayCluster"
)

type ClusterReconciler struct {
	PlatformCluster         *clusters.Cluster
	eventRecorder           events.EventRecorder
	ProviderName            string
	ProviderNamespace       string
	ClusterAccessReconciler accesslib.ClusterAccessReconciler
}

func NewClusterReconciler(platformCluster *clusters.Cluster, recorder events.EventRecorder, providerName, providerNamespace string) *ClusterReconciler {
	return &ClusterReconciler{
		PlatformCluster:   platformCluster,
		eventRecorder:     recorder,
		ProviderName:      providerName,
		ProviderNamespace: providerNamespace,
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

	res, err := r.reconcile(ctx, req)

	retryable := &utils.RetryableError{}
	if errors.As(err, &retryable) {
		log.Info(fmt.Sprintf("Handling retryable error: %s", retryable.Unwrap()), "RequeueAfter", retryable.RequeueAfter)
		return ctrl.Result{RequeueAfter: retryable.RequeueAfter}, nil
	}

	return res, err
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

	gwMgr, err := r.buildGatewayManager(ctx, req, c)
	if err != nil {
		return ctrl.Result{}, errors.Join(errFailedToBuildGatewayManager, err)
	}

	if !c.DeletionTimestamp.IsZero() || !r.enabledForCluster(c) {
		// delete gateway resources
		if err := gwMgr.Cleanup(ctx); err != nil {
			if utils.IsRemainingResourcesError(err) {
				r.eventRecorder.Eventf(c, nil, corev1.EventTypeNormal, reasonRemainingResources, actionUninstallGateway, err.Error())
			}
			return ctrl.Result{}, err
		}

		// uninstall gateway
		if err := gwMgr.Uninstall(ctx); err != nil {
			if utils.IsRemainingResourcesError(err) {
				r.eventRecorder.Eventf(c, nil, corev1.EventTypeNormal, reasonRemainingResources, actionUninstallGateway, err.Error())
			}
			return ctrl.Result{}, err
		}

		result, err := r.ClusterAccessReconciler.ReconcileDelete(ctx, req)
		if err != nil {
			log.Error(err, "failed to reconcile access/cluster request deletion")
			return result, err
		}
		if result.RequeueAfter > 0 {
			return result, nil
		}

		if controllerutil.RemoveFinalizer(c, gatewayv1alpha1.GatewayFinalizerOnCluster) {
			if err := r.PlatformCluster.Client().Update(ctx, c); err != nil {
				return ctrl.Result{}, err
			}
		}

		r.eventRecorder.Eventf(c, nil, corev1.EventTypeNormal, reasonGatewayUninstalled, actionUninstallGateway, "Gateway uninstalled successfully")
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

	r.eventRecorder.Eventf(c, nil, corev1.EventTypeNormal, reasonGatewayInstalled, actionInstallGateway, "Gateway installed successfully")
	return ctrl.Result{RequeueAfter: 1 * time.Hour}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clustersv1alpha1.Cluster{}).
		Watches(&gatewayv1alpha1.GatewayServiceConfig{}, r.mapGatewayServiceConfigToClusters()).
		Complete(r)
}

func (r *ClusterReconciler) buildGatewayManager(ctx context.Context, req reconcile.Request, c *clustersv1alpha1.Cluster) (*envoy.Gateway, error) {
	log := logging.FromContextOrPanic(ctx)
	log.Info("Creating or updating AccessRequest to get access to Cluster")

	res, err := r.ClusterAccessReconciler.Reconcile(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.RequeueAfter > 0 {
		return nil, utils.NewRetryableError(errClusterAccessNotYetAvailable, res.RequeueAfter)
	}

	ar, err := r.ClusterAccessReconciler.AccessRequest(ctx, req, clusterId)
	if err != nil {
		return nil, errors.Join(errFailedToGetAccessRequest, err)
	}

	access, err := r.ClusterAccessReconciler.Access(ctx, req, clusterId)
	if err != nil {
		return nil, errors.Join(errFailedToGetClusterAccess, err)
	}

	clusterClient := access.Client()
	utilruntime.Must(gatewayv1.Install(clusterClient.Scheme()))

	cfg, err := r.getGatewayServiceConfig(ctx, r.ProviderName)
	if err != nil {
		return nil, err
	}

	gw := &envoy.Gateway{
		Cluster:        c,
		EnvoyConfig:    cfg.Spec.EnvoyGateway,
		GatewayConfig:  cfg.Spec.Gateway,
		DNSConfig:      cfg.Spec.DNS,
		PlatformClient: r.PlatformCluster.Client(),
		ClusterClient:  access.Client(),
		FluxKubeconfig: &fluxmeta.KubeConfigReference{
			SecretRef: &fluxmeta.SecretKeyReference{
				Name: ar.Status.SecretRef.Name,
				Key:  clustersv1alpha1.SecretKeyKubeconfig,
			},
		},
	}
	return gw, nil
}

func (r *ClusterReconciler) shouldReconcile(cluster *clustersv1alpha1.Cluster) bool {
	return controllerutil.ContainsFinalizer(cluster, gatewayv1alpha1.GatewayFinalizerOnCluster) || r.enabledForCluster(cluster)
}

func (r *ClusterReconciler) enabledForCluster(cluster *clustersv1alpha1.Cluster) bool {
	ctx := context.Background()
	cfg, err := r.getGatewayServiceConfig(ctx, r.ProviderName)
	if err != nil {
		return false
	}

	for _, ct := range cfg.Spec.Clusters {
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

// mapGatewayServiceConfigToClusters returns an event handler that maps GatewayServiceConfig updates to reconciliation requests for matching  clusters.
func (r *ClusterReconciler) mapGatewayServiceConfigToClusters() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		log := logging.FromContextOrPanic(ctx)

		gatewayServiceConfig, ok := obj.(*gatewayv1alpha1.GatewayServiceConfig)
		if !ok {
			return []reconcile.Request{}
		}
		// required
		if gatewayServiceConfig.Name != r.ProviderName {
			return []reconcile.Request{}
		}

		log.Info("GatewayServiceConfig was updated, re-enqueueing matching cluster resources", "configName", gatewayServiceConfig.Name)

		clusters := &clustersv1alpha1.ClusterList{}
		if err := r.PlatformCluster.Client().List(ctx, clusters); err != nil {
			log.Error(err, "failed to list clusters")
			return []reconcile.Request{}
		}

		var requests []reconcile.Request
		for _, cluster := range clusters.Items {
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
	})
}

// getGatewayServiceConfig fetches the GatewayServiceConfig by name.
func (r *ClusterReconciler) getGatewayServiceConfig(ctx context.Context, gscName string) (*gatewayv1alpha1.GatewayServiceConfig, error) {
	config := &gatewayv1alpha1.GatewayServiceConfig{}
	err := r.PlatformCluster.Client().Get(ctx, types.NamespacedName{Name: gscName}, config)
	if err != nil {
		return nil, fmt.Errorf("failed to get GatewayServiceConfig '%s': %w", gscName, err)
	}
	return config, nil
}
