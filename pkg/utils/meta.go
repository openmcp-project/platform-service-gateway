package utils

import (
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ObjectIdentifier(obj client.Object) string {
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = reflect.TypeOf(obj).Elem().Name()
	}

	name := obj.GetName()
	namespace := obj.GetNamespace()

	if namespace == "" {
		return fmt.Sprintf("%s/%s", kind, name)
	} else {
		return fmt.Sprintf("%s/%s/%s", kind, namespace, name)
	}
}
