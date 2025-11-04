package utils

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func TestRetryableError(t *testing.T) {
	wrapped := errors.New("example error")
	err := NewRetryableError(wrapped, time.Minute)

	assert.IsType(t, &RetryableError{}, err)
	assert.True(t, errors.Is(err, &RetryableError{}), "error is not RetryableError")
	assert.True(t, errors.Is(err, wrapped), "RetryableError does not contain wrapped error")
	assert.False(t, errors.Is(err, fs.ErrClosed), "RetryableError matches unrelated error")

	re := &RetryableError{}
	if assert.True(t, errors.As(err, &re)) {
		assert.Equal(t, wrapped, re.Err)
		assert.Equal(t, wrapped.Error(), re.Error())
		assert.Equal(t, time.Minute, re.RequeueAfter)
	}
}

func TestRemainingResourcesError(t *testing.T) {
	objs := []client.Object{
		&corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "example",
			},
		},
		&corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "foo",
				Namespace: "example",
			},
		},
	}
	err := NewRemainingResourcesError(time.Minute, objs...)

	assert.True(t, errors.Is(err, &RemainingResourcesError{}), "error is not RemainingResourcesError")
	assert.True(t, errors.Is(err, &RetryableError{}), "error is not RetryableError")
	assert.False(t, errors.Is(err, fs.ErrClosed), "RemainingResourcesError matches unrelated error")

	rr := &RemainingResourcesError{}
	if assert.True(t, errors.As(err, &rr)) {
		assert.EqualValues(t, objs, rr.Objects)
		assert.Equal(t, "deletion of the following resources is still pending: [Namespace/example, Secret/example/foo]", rr.Error())
	}

	re := &RetryableError{}
	if assert.True(t, errors.As(err, &re)) {
		assert.Equal(t, time.Minute, re.RequeueAfter)
	}
}

func TestIsCRDNotFoundError(t *testing.T) {
	testCases := []struct {
		desc     string
		err      error
		expected bool
	}{
		{
			desc:     "should return false when err is nil",
			err:      nil,
			expected: false,
		},
		{
			desc:     "should return false when err is NoResourceMatchError",
			err:      &meta.NoResourceMatchError{},
			expected: true,
		},
		{
			desc:     "should return false when err is NoKindMatchError",
			err:      &meta.NoKindMatchError{},
			expected: true,
		},
		{
			desc:     "resource discovery failed error",
			err:      &apiutil.ErrResourceDiscoveryFailed{},
			expected: true,
		},
		{
			desc: "not found error",
			err: &apiutil.ErrResourceDiscoveryFailed{
				schema.GroupVersion{}: apierrors.NewNotFound(schema.GroupResource{}, ""),
			},
			expected: true,
		},
		{
			desc: "multiple not found error",
			err: &apiutil.ErrResourceDiscoveryFailed{
				schema.GroupVersion{}: apierrors.NewNotFound(schema.GroupResource{}, ""),
				schema.GroupVersion{}: apierrors.NewNotFound(schema.GroupResource{}, ""),
			},
			expected: true,
		},
		{
			desc: "Not found and forbidden error",
			err: &apiutil.ErrResourceDiscoveryFailed{
				schema.GroupVersion{}: apierrors.NewNotFound(schema.GroupResource{}, ""),
				schema.GroupVersion{}: apierrors.NewForbidden(schema.GroupResource{}, "", nil),
			},
			expected: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			actual := IsCRDNotFoundError(tC.err)
			assert.Equal(t, tC.expected, actual)
		})
	}
}
