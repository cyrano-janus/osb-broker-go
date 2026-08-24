package definition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// ErrNotFound mirrors broker.ErrNotFound semantics without an import cycle.
var ErrNotFound = errors.New("not found")

// OperatorClient performs the generic CR lifecycle against arbitrary
// operator APIs (Phase 2.3/2.4/2.5): apply, delete, readiness lookup and
// secret reads.
type OperatorClient struct {
	Client  client.Client
	Dynamic dynamic.Interface
	Scheme  *runtime.Scheme
}

// NewOperatorClient builds an OperatorClient around a controller-runtime client.
func NewOperatorClient(k8sClient client.Client) *OperatorClient {
	return &OperatorClient{Client: k8sClient}
}

// ApplyCR parses rendered YAML into an unstructured object and creates or
// updates it (Create-or-Update with resourceVersion preservation).
func (o *OperatorClient) ApplyCR(ctx context.Context, apiVersion, kind, namespace, manifest string) error {
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal([]byte(manifest), &obj.Object); err != nil {
		return fmt.Errorf("decode rendered manifest: %w", err)
	}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	if obj.GetNamespace() == "" {
		obj.SetNamespace(namespace)
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	err := o.Client.Get(ctx, client.ObjectKeyFromObject(obj), existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := o.Client.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create %s %q: %w", kind, obj.GetName(), err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get %s %q: %w", kind, obj.GetName(), err)
	default:
		// Preserve resourceVersion for update; keep spec/status from new.
		obj.SetResourceVersion(existing.GetResourceVersion())
		if err := o.Client.Update(ctx, obj); err != nil {
			return fmt.Errorf("update %s %q: %w", kind, obj.GetName(), err)
		}
		return nil
	}
}

// DeleteCR removes the custom resource by GVK and name.
func (o *OperatorClient) DeleteCR(ctx context.Context, apiVersion, kind, namespace, name string) error {
	obj := &unstructured.Unstructured{}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return fmt.Errorf("parse apiVersion: %w", err)
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: kind})
	obj.SetNamespace(namespace)
	obj.SetName(name)

	err = o.Client.Delete(ctx, obj)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %s %q: %w", kind, name, err)
	}
	return nil
}

// GetCR fetches the full custom resource of an instance.
func (o *OperatorClient) GetCR(ctx context.Context, apiVersion, kind, namespace, name string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, fmt.Errorf("parse apiVersion: %w", err)
	}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: kind})
	u.SetNamespace(namespace)
	u.SetName(name)

	err = o.Client.Get(ctx, client.ObjectKeyFromObject(u), u)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s %q", ErrNotFound, kind, name)
		}
		return nil, fmt.Errorf("get %s %q: %w", kind, name, err)
	}
	return u, nil
}

// GetCRStatus fetches the current status subobject of the instance's CR.
func (o *OperatorClient) GetCRStatus(ctx context.Context, apiVersion, kind, namespace, name string) (map[string]interface{}, error) {
	u := &unstructured.Unstructured{}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, fmt.Errorf("parse apiVersion: %w", err)
	}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: kind})
	u.SetNamespace(namespace)
	u.SetName(name)

	err = o.Client.Get(ctx, client.ObjectKeyFromObject(u), u)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s %q", ErrNotFound, kind, name)
		}
		return nil, fmt.Errorf("get %s %q: %w", kind, name, err)
	}
	st := u.UnstructuredContent()["status"]
	m, ok := st.(map[string]interface{})
	if !ok {
		m = map[string]interface{}{}
	}
	return m, nil
}

// ReadSecret loads a Secret and returns its decoded data map.
func (o *OperatorClient) ReadSecret(ctx context.Context, namespace, name string) (map[string][]byte, error) {
	s := &corev1.Secret{}
	err := o.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, s)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: secret %q", ErrNotFound, name)
		}
		return nil, fmt.Errorf("get secret %q: %w", name, err)
	}
	return s.Data, nil
}

// jsonField is a helper for tests / debugging of unstructured content.
func jsonField(u *unstructured.Unstructured, field string) string {
	b, _ := json.Marshal(u.UnstructuredContent()[field])
	return string(b)
}
