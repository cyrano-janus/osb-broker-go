package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"github.com/example/osb-broker/internal/definition"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrNotFound is returned by Get/Delete lookups when the entity does not
// exist in the store.
var ErrNotFound = errors.New("not found")

const (
	stateConfigMapName = "osb-broker-state"

	prefixInstance = "instance_"
	prefixBinding  = "binding_"
)

// K8sStateStore persists instances and bindings as JSON entries in a single
// ConfigMap. Kubernetes replicates and stores the ConfigMap on disk, so the
// state survives pod restarts and rescheduling — no external database needed.
type K8sStateStore struct {
	client    client.Client
	namespace string
}

// NewK8sStateStore returns a StateStore backed by a ConfigMap named
// "osb-broker-state" in the given namespace (created on first write).
func NewK8sStateStore(c client.Client, namespace string) *K8sStateStore {
	return &K8sStateStore{client: c, namespace: namespace}
}

// NewK8sClient builds a controller-runtime client from the given rest config.
func NewK8sClient(cfg *rest.Config) (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	return client.New(cfg, client.Options{Scheme: scheme})
}

// NewOperatorClient builds an OperatorClient from a controller-runtime client.
func NewOperatorClient(k8sClient client.Client) *definition.OperatorClient {
	return &definition.OperatorClient{Client: k8sClient}
}

// stateData is the in-ConfigMap representation of all stored entities.
type stateData struct {
	Instances map[string]*Instance `json:"instances"`
	Bindings  map[string]*Binding  `json:"bindings"`
}

func newStateData() *stateData {
	return &stateData{
		Instances: make(map[string]*Instance),
		Bindings:  make(map[string]*Binding),
	}
}

func clientObjectKey(namespace, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: namespace, Name: name}
}

// load reads and decodes the state ConfigMap. A missing ConfigMap yields an
// empty state (first-use), not an error.
func (k *K8sStateStore) load(ctx context.Context) (*stateData, error) {
	cm := &corev1.ConfigMap{}
	err := k.client.Get(ctx, clientObjectKey(k.namespace, stateConfigMapName), cm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return newStateData(), nil
		}
		return nil, fmt.Errorf("load state configmap: %w", err)
	}
	raw, ok := cm.Data["state.json"]
	if !ok || raw == "" {
		return newStateData(), nil
	}
	sd := newStateData()
	if err := json.Unmarshal([]byte(raw), sd); err != nil {
		return nil, fmt.Errorf("decode state configmap: %w", err)
	}
	if sd.Instances == nil {
		sd.Instances = make(map[string]*Instance)
	}
	if sd.Bindings == nil {
		sd.Bindings = make(map[string]*Binding)
	}
	return sd, nil
}

// save encodes and writes the state back to the ConfigMap, creating it if
// necessary.
func (k *K8sStateStore) save(ctx context.Context, sd *stateData) error {
	raw, err := json.Marshal(sd)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stateConfigMapName,
			Namespace: k.namespace,
		},
		Data: map[string]string{"state.json": string(raw)},
	}

	existing := &corev1.ConfigMap{}
	err = k.client.Get(ctx, clientObjectKey(k.namespace, stateConfigMapName), existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := k.client.Create(ctx, cm); err != nil {
			return fmt.Errorf("create state configmap: %w", err)
		}
	case err != nil:
		return fmt.Errorf("get state configmap: %w", err)
	default:
		cm.ResourceVersion = existing.ResourceVersion
		if err := k.client.Update(ctx, cm); err != nil {
			return fmt.Errorf("update state configmap: %w", err)
		}
	}
	return nil
}

func (k *K8sStateStore) PutInstance(ctx context.Context, i *Instance) error {
	sd, err := k.load(ctx)
	if err != nil {
		return err
	}
	cp := *i
	sd.Instances[i.ID] = &cp
	return k.save(ctx, sd)
}

func (k *K8sStateStore) GetInstance(ctx context.Context, id string) (*Instance, error) {
	sd, err := k.load(ctx)
	if err != nil {
		return nil, err
	}
	i, ok := sd.Instances[id]
	if !ok {
		return nil, fmt.Errorf("%w: instance %q", ErrNotFound, id)
	}
	cp := *i
	return &cp, nil
}

func (k *K8sStateStore) DeleteInstance(ctx context.Context, id string) error {
	sd, err := k.load(ctx)
	if err != nil {
		return err
	}
	delete(sd.Instances, id)
	return k.save(ctx, sd)
}

func (k *K8sStateStore) PutBinding(ctx context.Context, b *Binding) error {
	sd, err := k.load(ctx)
	if err != nil {
		return err
	}
	cp := *b
	sd.Bindings[b.ID] = &cp
	return k.save(ctx, sd)
}

func (k *K8sStateStore) GetBinding(ctx context.Context, id string) (*Binding, error) {
	sd, err := k.load(ctx)
	if err != nil {
		return nil, err
	}
	b, ok := sd.Bindings[id]
	if !ok {
		return nil, fmt.Errorf("%w: binding %q", ErrNotFound, id)
	}
	cp := *b
	return &cp, nil
}

func (k *K8sStateStore) DeleteBinding(ctx context.Context, id string) error {
	sd, err := k.load(ctx)
	if err != nil {
		return err
	}
	delete(sd.Bindings, id)
	return k.save(ctx, sd)
}

func (k *K8sStateStore) ListBindingsByInstance(ctx context.Context, instanceID string) ([]*Binding, error) {
	sd, err := k.load(ctx)
	if err != nil {
		return nil, err
	}
	var out []*Binding
	for _, b := range sd.Bindings {
		if b.InstanceID == instanceID {
			cp := *b
			out = append(out, &cp)
		}
	}
	return out, nil
}
