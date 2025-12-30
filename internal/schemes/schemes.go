package schemes

import (
	"k8s.io/apimachinery/pkg/runtime"

	fluxhelmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxsourcev1 "github.com/fluxcd/source-controller/api/v1"
	providerv1alpha1 "github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	providerscheme "github.com/openmcp-project/platform-service-gateway/api/install"
)

var (
	Platform = runtime.NewScheme()
	Target   = runtime.NewScheme()
)

func init() {
	// Install APIs into Platform scheme
	providerscheme.InstallOperatorAPIsPlatform(Platform)
	utilruntime.Must(providerv1alpha1.AddToScheme(Platform))
	utilruntime.Must(fluxsourcev1.AddToScheme(Platform))
	utilruntime.Must(fluxhelmv2.AddToScheme(Platform))

	// Install APIs into Target scheme
	utilruntime.Must(clientgoscheme.AddToScheme(Target))
	utilruntime.Must(gatewayv1.Install(Target))
}
