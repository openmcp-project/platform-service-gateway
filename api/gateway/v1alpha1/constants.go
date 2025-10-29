package v1alpha1

import (
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
)

const (
	OperationAnnotation = "gateway." + openmcpconst.OperationAnnotation

	GatewayFinalizerOnCluster = "platformservice." + openmcpconst.OpenMCPGroupName + "/gateway"
)
