package broker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newK8sTestStore(t *testing.T) *K8sStateStore {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	return NewK8sStateStore(c, "osb-state-test")
}

func TestK8sStateStore_PutGetInstance(t *testing.T) {
	s := newK8sTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.PutInstance(ctx, newTestInstance("inst-1")))

	got, err := s.GetInstance(ctx, "inst-1")
	require.NoError(t, err)
	assert.Equal(t, "service-1", got.ServiceID)
	assert.Equal(t, "plan-free", got.PlanID)
}

func TestK8sStateStore_GetInstanceNotFound(t *testing.T) {
	s := newK8sTestStore(t)
	_, err := s.GetInstance(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestK8sStateStore_DeleteInstance(t *testing.T) {
	s := newK8sTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.PutInstance(ctx, newTestInstance("inst-1")))
	require.NoError(t, s.DeleteInstance(ctx, "inst-1"))
	_, err := s.GetInstance(ctx, "inst-1")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestK8sStateStore_BindingLifecycle(t *testing.T) {
	s := newK8sTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.PutBinding(ctx, newTestBinding("b1", "inst-1")))
	got, err := s.GetBinding(ctx, "b1")
	require.NoError(t, err)
	assert.Equal(t, "inst-1", got.InstanceID)

	bindings, err := s.ListBindingsByInstance(ctx, "inst-1")
	require.NoError(t, err)
	assert.Len(t, bindings, 1)

	require.NoError(t, s.DeleteBinding(ctx, "b1"))
	_, err = s.GetBinding(ctx, "b1")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestK8sStateStore_StateLivesInConfigMap(t *testing.T) {
	s := newK8sTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.PutInstance(ctx, newTestInstance("cm-check")))

	var cm corev1.ConfigMap
	err := s.client.Get(ctx, clientObjectKey(s.namespace, stateConfigMapName), &cm)
	require.NoError(t, err, "state must live in a ConfigMap in-cluster")
	assert.Contains(t, cm.Data["state.json"], `"cm-check"`)
}

// silence unused warnings for apierrors (used indirectly via ErrorIs paths)
var _ = apierrors.IsNotFound
