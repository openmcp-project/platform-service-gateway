package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestObjectIdentifier(t *testing.T) {
	testCases := []struct {
		desc     string
		obj      client.Object
		expected string
	}{
		{
			desc: "GatewayClass",
			obj: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			expected: "GatewayClass/test",
		},
		{
			desc: "Gateway",
			obj: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
			},
			expected: "Gateway/bar/foo",
		},
		{
			desc:     "EnvoyProxy (unstructured)",
			obj:      unstructuredObj(),
			expected: "Gateway/bar/foo",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			actual := ObjectIdentifier(tC.obj)
			assert.Equal(t, tC.expected, actual)
		})
	}
}

func unstructuredObj() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.envoyproxy.io",
		Version: "v1alpha1",
		Kind:    "EnvoyProxy",
	})
	obj.SetName("foo")
	obj.SetNamespace("bar")
	return obj
}
