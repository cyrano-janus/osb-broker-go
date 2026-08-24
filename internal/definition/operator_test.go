package definition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestOperatorClient(t *testing.T) (*OperatorClient, *runtime.Scheme) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	ctrlClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	gvr := schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}
	_ = gvr
	listOptions := metav1.ListOptions{}
	_ = listOptions
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			gvr: "ClusterList",
		})
	return &OperatorClient{
		Client:  ctrlClient,
		Dynamic: dyn,
		Scheme:  scheme,
	}, scheme
}

func testDefinition(t *testing.T) *ServiceDefinition {
	t.Helper()
	sd, err := Parse([]byte(validYAML))
	require.NoError(t, err)
	return sd
}

func TestOperatorClient_ApplyCreatesCR(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	ctx := context.Background()

	rendered, err := RenderProvision(sd, "inst-abc", map[string]interface{}{"storageSize": "1Gi", "instances": 1})
	require.NoError(t, err)

	err = oc.ApplyCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", rendered)
	require.NoError(t, err)

	// CR muss existieren (List via controller-runtime client mit GVK)
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "ClusterList"})
	err = oc.Client.List(ctx, u, client.InNamespace("default"))
	if err != nil {
		t.Fatalf("list CRs: %v", err)
	}
	assert.Len(t, u.Items, 1)
	assert.Equal(t, "inst-abc", u.Items[0].GetName())
}

func TestOperatorClient_DeleteCRRemovesIt(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	ctx := context.Background()

	rendered, err := RenderProvision(sd, "inst-del", map[string]interface{}{"storageSize": "1Gi", "instances": 1})
	require.NoError(t, err)
	require.NoError(t, oc.ApplyCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", rendered))

	err = oc.DeleteCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "inst-del")
	require.NoError(t, err)

	u := &unstructured.Unstructured{}
	gv, gerr := schema.ParseGroupVersion(sd.Spec.Provision.APIVersion)
	require.NoError(t, gerr)
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: sd.Spec.Provision.Kind})
	err = oc.Client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "inst-del"}, u)
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

func TestOperatorClient_ReadSecret(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	ctx := context.Background()

	secret := &corev1.Secret{
		Data: map[string][]byte{
			"username": []byte("pg-user"),
			"password": []byte("pg-pass"),
			"host":     []byte("pg-host"),
		},
	}
	secret.Name = "inst-abc-app"
	secret.Namespace = "default"
	require.NoError(t, oc.Client.Create(ctx, secret))

	data, err := oc.ReadSecret(ctx, "default", "inst-abc-app")
	require.NoError(t, err)
	assert.Equal(t, "pg-user", string(data["username"]))
	assert.Equal(t, "pg-pass", string(data["password"]))
}

func TestOperatorClient_ReadSecretNotFound(t *testing.T) {
	oc, _ := newTestOperatorClient(t)
	_, err := oc.ReadSecret(context.Background(), "default", "missing-secret")
	assert.ErrorIs(t, err, ErrNotFound)
}
