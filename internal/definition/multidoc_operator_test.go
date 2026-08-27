package definition

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newFakeClient builds a controller-runtime fake client pre-registered for
// unstructured objects (scheme already has clientgoscheme).
func newMultiDocFakeClient(t *testing.T) *OperatorClient {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ctrl := fake.NewClientBuilder().WithScheme(scheme).Build()
	return &OperatorClient{Client: ctrl, Scheme: scheme}
}

func TestApplyManifests_MultiDoc_AppliesAllInOrder(t *testing.T) {
	c := newMultiDocFakeClient(t)
	ctx := context.Background()

	rendered := `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{NAME}}-first
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{NAME}}-second
`
	rendered = strings.ReplaceAll(rendered, "{{NAME}}", "osb-inst")

	applied, err := c.ApplyManifests(ctx, "v1", "ConfigMap", "default", rendered)
	if err != nil {
		t.Fatalf("ApplyManifests: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied objects, got %d", len(applied))
	}
	if applied[0] != "osb-inst-first" || applied[1] != "osb-inst-second" {
		t.Fatalf("applied names out of order or wrong: %q", applied)
	}

	// Both objects must exist in the cluster.
	for _, name := range applied {
		cm := &corev1.ConfigMap{}
		err := c.Client.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, cm)
		if err != nil {
			t.Fatalf("object %q not found after apply: %v", name, err)
		}
	}
}

func TestApplyManifests_NamespacedDefaulting(t *testing.T) {
	// A doc without metadata.namespace gets the target namespace.
	c := newMultiDocFakeClient(t)
	rendered := `apiVersion: v1
kind: ConfigMap
metadata:
  name: ns-defaulted
`
	if _, err := c.ApplyManifests(context.Background(), "v1", "ConfigMap", "target-ns", rendered); err != nil {
		t.Fatalf("ApplyManifests: %v", err)
	}
	cm := &corev1.ConfigMap{}
	if err := c.Client.Get(context.Background(), client.ObjectKey{Namespace: "target-ns", Name: "ns-defaulted"}, cm); err != nil {
		t.Fatalf("not found in target-ns: %v", err)
	}
}

func TestDeleteManifests_RemovesAllAppliedObjects(t *testing.T) {
	c := newMultiDocFakeClient(t)
	ctx := context.Background()
	rendered := `apiVersion: v1
kind: ConfigMap
metadata:
  name: osb-del-a
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: osb-del-b
`
	names, err := c.ApplyManifests(ctx, "v1", "ConfigMap", "default", rendered)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	deleted, err := c.DeleteManifestsByNames(ctx, "v1", "ConfigMap", "default", names)
	if err != nil {
		t.Fatalf("DeleteManifestsByNames: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deletions, got %d", len(deleted))
	}

	for _, name := range names {
		cm := &corev1.ConfigMap{}
		err := c.Client.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, cm)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("object %q still exists (err=%v)", name, err)
		}
	}
}

func TestDeleteManifests_IdempotentOnMissingObjects(t *testing.T) {
	// Deleting already-gone objects must not error (mirrors DeleteCR).
	c := newMultiDocFakeClient(t)
	_, err := c.DeleteManifestsByNames(context.Background(), "v1", "ConfigMap", "default",
		[]string{"never-existed"})
	if err != nil {
		t.Fatalf("delete of missing object must be idempotent: %v", err)
	}
}

var _ = metav1.ObjectMeta{} // keep import if unused in future edits
