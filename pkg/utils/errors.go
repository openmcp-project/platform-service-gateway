package utils

import (
	"errors"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// ----- RetryableError -----

func NewRetryableError(err error, requeueAfter time.Duration) error {
	return &RetryableError{
		Err:          err,
		RequeueAfter: requeueAfter,
	}
}

var _ error = &RetryableError{}

// RetryableError is an error that can be resolved by retrying after a certain time (`RequeueAfter`).
type RetryableError struct {
	// Err is the wrapped error.
	Err error

	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	RequeueAfter time.Duration
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

func (*RetryableError) Is(target error) bool {
	_, ok := target.(*RetryableError)
	return ok
}

// ----- RemainingResourcesError -----

// NewRemainingResourcesError creates a new RemainingResourcesError wrapped in a RetryableError.
func NewRemainingResourcesError(requeueAfter time.Duration, objs ...client.Object) error {
	return &RetryableError{
		RequeueAfter: requeueAfter,
		Err: &RemainingResourcesError{
			Objects: objs,
		},
	}
}

var _ error = &RemainingResourcesError{}

// RemainingResourcesError occurs when an operation could not be finished because there are resources pending deletion,
// usually during uninstall processes.
type RemainingResourcesError struct {
	Objects []client.Object
}

// RemainingResourcesError implements error.
func (r *RemainingResourcesError) Error() string {
	ids := make([]string, len(r.Objects))
	for i, obj := range r.Objects {
		ids[i] = ObjectIdentifier(obj)
	}
	return fmt.Sprintf("deletion of the following resources is still pending: [%s]", strings.Join(ids, ", "))
}

func (*RemainingResourcesError) Is(target error) bool {
	_, ok := target.(*RemainingResourcesError)
	return ok
}

func IsRemainingResourcesError(err error) bool {
	return errors.Is(err, &RemainingResourcesError{})
}

// ----- Not found error -----

// IsCRDNotFoundError checks if the given error is a CRD not found error.
func IsCRDNotFoundError(err error) bool {
	// check if err tree contains a "No[Kind|Resource]MatchError" error.
	if meta.IsNoMatchError(err) {
		return true
	}

	// check if err tree contains a "ErrResourceDiscoveryFailed" error.
	var rdfErr *apiutil.ErrResourceDiscoveryFailed
	if !errors.As(err, &rdfErr) {
		return false
	}

	// all wrapped errors must be "NotFound" errors.
	// only then the entire "ErrResourceDiscoveryFailed" is considered as "CRD not found".
	for _, wrappedErr := range *rdfErr {
		if !apierrors.IsNotFound(wrappedErr) {
			return false
		}
	}

	return true
}
