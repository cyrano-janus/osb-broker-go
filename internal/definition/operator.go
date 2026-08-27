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

// GetSecretObj exposes secret reading for tests and internal callers.
func (o *OperatorClient) GetSecretObj(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	s.SetNamespace(namespace)
	s.SetName(name)
	if err := o.Client.Get(ctx, client.ObjectKeyFromObject(s), s); err != nil {
		return nil, err
	}
	return s, nil
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

// ObjectRef identifies a single applied object by its own GVK, namespace and
// name. A multi-doc template may mix kinds, so the GVK of an applied object
// cannot be inferred from the definition's provision header at delete time —
// it has to be remembered per object.
type ObjectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// decodeManifests splits a (possibly multi-document) rendered YAML string and
// decodes every document into an unstructured object. Documents that omit
// apiVersion/kind/namespace inherit the passed defaults; a document without
// metadata.name is rejected.
func decodeManifests(defaultAPIVersion, defaultKind, namespace, rendered string) ([]*unstructured.Unstructured, error) {
	docs := SplitManifests(rendered)
	objs := make([]*unstructured.Unstructured, 0, len(docs))
	for _, doc := range docs {
		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj.Object); err != nil {
			return nil, fmt.Errorf("decode rendered manifest: %w", err)
		}
		if obj.GetAPIVersion() == "" {
			obj.SetAPIVersion(defaultAPIVersion)
		}
		if obj.GetKind() == "" {
			obj.SetKind(defaultKind)
		}
		if obj.GetNamespace() == "" {
			obj.SetNamespace(namespace)
		}
		if obj.GetName() == "" {
			return nil, fmt.Errorf("manifest without metadata.name (kind %s)", obj.GetKind())
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

// ApplyManifests applies a (possibly multi-document) rendered YAML string and
// returns the applied object names in document order. Convenience wrapper
// around ApplyManifestRefs for callers that only track names.
func (o *OperatorClient) ApplyManifests(ctx context.Context, defaultAPIVersion, defaultKind, namespace, rendered string) ([]string, error) {
	refs, err := o.ApplyManifestRefs(ctx, defaultAPIVersion, defaultKind, namespace, rendered)
	return refNames(refs), err
}

// ApplyManifestRefs applies every document of a rendered manifest in order
// (create-or-update, same semantics as ApplyCR) and returns one reference per
// applied object. The per-doc apiVersion/kind from the manifest win over the
// defaults, so the refs describe what was really created.
func (o *OperatorClient) ApplyManifestRefs(ctx context.Context, defaultAPIVersion, defaultKind, namespace, rendered string) ([]ObjectRef, error) {
	objs, err := decodeManifests(defaultAPIVersion, defaultKind, namespace, rendered)
	if err != nil {
		return nil, err
	}
	var applied []ObjectRef
	for _, obj := range objs {
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(obj.GroupVersionKind())
		err := o.Client.Get(ctx, client.ObjectKeyFromObject(obj), existing)
		switch {
		case apierrors.IsNotFound(err):
			if err := o.Client.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
				return applied, fmt.Errorf("create %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
		case err != nil:
			return applied, fmt.Errorf("get %s %q: %w", obj.GetKind(), obj.GetName(), err)
		default:
			obj.SetResourceVersion(existing.GetResourceVersion())
			if err := o.Client.Update(ctx, obj); err != nil {
				return applied, fmt.Errorf("update %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
		}
		applied = append(applied, ObjectRef{
			APIVersion: obj.GetAPIVersion(),
			Kind:       obj.GetKind(),
			Namespace:  obj.GetNamespace(),
			Name:       obj.GetName(),
		})
	}
	return applied, nil
}

// ManifestsUpToDate reports whether every document of the rendered manifest
// already exists in the cluster carrying the desired state. Callers use it to
// skip no-op updates: even a write that changes nothing bumps resourceVersion
// and wakes up the operator's reconcile loop. Anything that cannot be proven
// equal (missing object, unreadable, differing content) yields false — the
// safe answer is to apply.
func (o *OperatorClient) ManifestsUpToDate(ctx context.Context, defaultAPIVersion, defaultKind, namespace, rendered string) (bool, error) {
	objs, err := decodeManifests(defaultAPIVersion, defaultKind, namespace, rendered)
	if err != nil {
		return false, err
	}
	if len(objs) == 0 {
		return false, nil
	}
	for _, want := range objs {
		live := &unstructured.Unstructured{}
		live.SetGroupVersionKind(want.GroupVersionKind())
		if err := o.Client.Get(ctx, client.ObjectKeyFromObject(want), live); err != nil {
			return false, nil
		}
		if !crMatchesRendered(live, want) {
			return false, nil
		}
	}
	return true, nil
}

// DeleteManifestsByNames removes previously applied objects that all share the
// given GVK. Used for records written before applied refs were tracked; new
// records go through DeleteManifestRefs.
func (o *OperatorClient) DeleteManifestsByNames(ctx context.Context, defaultAPIVersion, defaultKind, namespace string, names []string) ([]string, error) {
	refs := make([]ObjectRef, 0, len(names))
	for _, name := range names {
		refs = append(refs, ObjectRef{APIVersion: defaultAPIVersion, Kind: defaultKind, Name: name})
	}
	return o.DeleteManifestRefs(ctx, namespace, refs)
}

// DeleteManifestRefs removes previously applied objects, each by its own GVK,
// so multi-doc templates with MIXED kinds (e.g. a ConfigMap plus a custom
// resource) are fully cleaned up. Missing objects are tolerated (idempotent
// delete). Returns the names actually processed.
func (o *OperatorClient) DeleteManifestRefs(ctx context.Context, namespace string, refs []ObjectRef) ([]string, error) {
	var deleted []string
	for _, ref := range refs {
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return deleted, fmt.Errorf("parse apiVersion %q: %w", ref.APIVersion, err)
		}
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gv.WithKind(ref.Kind))
		obj.SetName(ref.Name)
		if ref.Namespace != "" {
			obj.SetNamespace(ref.Namespace)
		} else {
			obj.SetNamespace(namespace)
		}

		if err := o.Client.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			return deleted, fmt.Errorf("delete %s %q: %w", ref.Kind, ref.Name, err)
		}
		deleted = append(deleted, ref.Name)
	}
	return deleted, nil
}

// refNames projects the names out of a ref list (document order preserved).
func refNames(refs []ObjectRef) []string {
	if len(refs) == 0 {
		return nil
	}
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.Name)
	}
	return names
}
