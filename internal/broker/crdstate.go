package broker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	osbv1 "github.com/example/osb-broker/internal/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CRDStateStore haelt Instanzen und Bindings als je ein Custom Resource.
//
// Der Vorgaenger legte alles in ein JSON-Dokument in einer ConfigMap. Damit
// war bei rund 500 Instanzen das 1-MiB-Limit erreicht, jeder Schreibzugriff
// schrieb den gesamten Zustand neu, und zwei gleichzeitige Schreiber
// ueberschrieben sich, weil die resourceVersion fuer den Update aus einem
// zweiten Get stammte und nicht aus dem, auf dem die Aenderung beruhte.
//
// Hier hat jeder Datensatz sein eigenes Objekt: kein Gesamtlimit, ein
// Schreibzugriff kostet einen Datensatz, und Konflikte betreffen nur den
// Datensatz, an dem wirklich zwei Schreiber arbeiten - abgefangen mit
// RetryOnConflict statt mit stillem Ueberschreiben.
type CRDStateStore struct {
	client    client.Client
	namespace string
}

// NewCRDStateStore gibt einen StateStore zurueck, der seine Objekte im
// angegebenen Namespace anlegt.
func NewCRDStateStore(c client.Client, namespace string) *CRDStateStore {
	return &CRDStateStore{client: c, namespace: namespace}
}

const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "osb-broker-go"

	// credentialsKey ist der Key im Credentials-Secret. Ein Blob statt eines
	// Keys je Feld: OSB-Credentials sind beliebiges JSON und koennen
	// verschachtelt sein, ein Secret kennt aber nur flache Byte-Werte.
	credentialsKey = "credentials.json"
)

// dns1123Label beschreibt, was Kubernetes als Objektnamen akzeptiert.
var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// resourceName leitet den Objektnamen aus einer OSB-ID ab.
//
// Ist die ID bereits ein gueltiger Name - der Normalfall, Cloud Foundry
// schickt UUIDs -, wird sie unveraendert benutzt, damit `kubectl get`
// ohne Uebersetzungstabelle lesbar bleibt. Sonst entscheidet ein Hash: eine
// Zeichen-fuer-Zeichen-Ersetzung koennte zwei verschiedene IDs auf denselben
// Namen abbilden und damit einen fremden Datensatz ueberschreiben.
//
// Die urspruengliche ID steht in jedem Fall in spec.id; Get vergleicht sie.
func resourceName(id string) string {
	if len(id) <= 63 && dns1123Label.MatchString(id) {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return "osb-" + hex.EncodeToString(sum[:])[:48]
}

func (s *CRDStateStore) key(id string) types.NamespacedName {
	return types.NamespacedName{Namespace: s.namespace, Name: resourceName(id)}
}

func (s *CRDStateStore) objectMeta(id string, labels map[string]string) metav1.ObjectMeta {
	l := map[string]string{managedByLabel: managedByValue}
	for k, v := range labels {
		l[k] = v
	}
	return metav1.ObjectMeta{Name: resourceName(id), Namespace: s.namespace, Labels: l}
}

// --- Instanzen ---

// PutInstance legt die Instanz an oder aktualisiert sie.
func (s *CRDStateStore) PutInstance(ctx context.Context, i *Instance) error {
	spec, err := instanceToSpec(i)
	if err != nil {
		return err
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cr osbv1.OSBServiceInstance
		err := s.client.Get(ctx, s.key(i.ID), &cr)
		switch {
		case apierrors.IsNotFound(err):
			cr = osbv1.OSBServiceInstance{ObjectMeta: s.objectMeta(i.ID, nil), Spec: spec}
			return s.client.Create(ctx, &cr)
		case err != nil:
			return fmt.Errorf("get instance %q: %w", i.ID, err)
		default:
			// Nur den Spec ersetzen: resourceVersion und alles, was jemand
			// sonst am Objekt annotiert hat, bleiben stehen.
			cr.Spec = spec
			return s.client.Update(ctx, &cr)
		}
	})
}

// GetInstance liest eine Instanz.
func (s *CRDStateStore) GetInstance(ctx context.Context, id string) (*Instance, error) {
	var cr osbv1.OSBServiceInstance
	if err := s.client.Get(ctx, s.key(id), &cr); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: instance %q", ErrNotFound, id)
		}
		return nil, fmt.Errorf("get instance %q: %w", id, err)
	}
	// Der Objektname ist nur abgeleitet; bei einer Hash-Kollision gehoerte
	// das Objekt einer anderen ID und darf nicht als diese gelten.
	if cr.Spec.ID != id {
		return nil, fmt.Errorf("%w: instance %q", ErrNotFound, id)
	}
	return specToInstance(&cr.Spec)
}

// DeleteInstance entfernt die Instanz. Eine nicht vorhandene Instanz ist kein
// Fehler, damit Deprovision idempotent bleibt.
func (s *CRDStateStore) DeleteInstance(ctx context.Context, id string) error {
	cr := osbv1.OSBServiceInstance{ObjectMeta: metav1.ObjectMeta{
		Name: resourceName(id), Namespace: s.namespace}}
	if err := s.client.Delete(ctx, &cr); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete instance %q: %w", id, err)
	}
	return nil
}

// --- Bindings ---

// PutBinding legt das Binding an oder aktualisiert es und schreibt die
// Credentials in ein eigenes Secret.
func (s *CRDStateStore) PutBinding(ctx context.Context, b *Binding) error {
	spec, err := bindingToSpec(b)
	if err != nil {
		return err
	}

	secretName := ""
	if len(b.Credentials) > 0 {
		secretName = resourceName(b.ID) + "-credentials"
	}
	spec.CredentialsSecret = secretName

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cr osbv1.OSBServiceBinding
		err := s.client.Get(ctx, s.key(b.ID), &cr)
		labels := map[string]string{osbv1.LabelInstance: resourceName(b.InstanceID)}
		switch {
		case apierrors.IsNotFound(err):
			cr = osbv1.OSBServiceBinding{ObjectMeta: s.objectMeta(b.ID, labels), Spec: spec}
			return s.client.Create(ctx, &cr)
		case err != nil:
			return fmt.Errorf("get binding %q: %w", b.ID, err)
		default:
			cr.Spec = spec
			if cr.Labels == nil {
				cr.Labels = map[string]string{}
			}
			for k, v := range labels {
				cr.Labels[k] = v
			}
			return s.client.Update(ctx, &cr)
		}
	})
	if err != nil {
		return err
	}

	if secretName == "" {
		// Credentials koennen verschwinden (Rebind ohne). Dann muss auch das
		// Secret weg, sonst bleibt es unreferenziert liegen.
		return s.deleteCredentialsSecret(ctx, resourceName(b.ID)+"-credentials")
	}
	return s.writeCredentialsSecret(ctx, b, secretName)
}

// GetBinding liest ein Binding samt Credentials.
func (s *CRDStateStore) GetBinding(ctx context.Context, id string) (*Binding, error) {
	var cr osbv1.OSBServiceBinding
	if err := s.client.Get(ctx, s.key(id), &cr); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: binding %q", ErrNotFound, id)
		}
		return nil, fmt.Errorf("get binding %q: %w", id, err)
	}
	if cr.Spec.ID != id {
		return nil, fmt.Errorf("%w: binding %q", ErrNotFound, id)
	}
	return s.specToBinding(ctx, &cr.Spec)
}

// DeleteBinding entfernt Binding und Credentials-Secret.
func (s *CRDStateStore) DeleteBinding(ctx context.Context, id string) error {
	cr := osbv1.OSBServiceBinding{ObjectMeta: metav1.ObjectMeta{
		Name: resourceName(id), Namespace: s.namespace}}
	if err := s.client.Delete(ctx, &cr); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete binding %q: %w", id, err)
	}
	// Explizit, nicht nur ueber die OwnerReference: die Garbage Collection
	// laeuft asynchron, und ein liegengebliebenes Secret enthaelt echte
	// Datenbank-Credentials.
	return s.deleteCredentialsSecret(ctx, resourceName(id)+"-credentials")
}

// ListBindingsByInstance liefert alle Bindings einer Instanz.
func (s *CRDStateStore) ListBindingsByInstance(ctx context.Context, instanceID string) ([]*Binding, error) {
	var list osbv1.OSBServiceBindingList
	// Serverseitig ueber das Label filtern statt alles zu laden und im
	// Speicher zu sortieren - bei ueber 1000 Instanzen waere Letzteres genau
	// der Aufwand, wegen dem der ConfigMap-Store aufgegeben wurde.
	err := s.client.List(ctx, &list,
		client.InNamespace(s.namespace),
		client.MatchingLabels{osbv1.LabelInstance: resourceName(instanceID)})
	if err != nil {
		return nil, fmt.Errorf("list bindings for instance %q: %w", instanceID, err)
	}

	out := make([]*Binding, 0, len(list.Items))
	for i := range list.Items {
		// Das Label traegt den abgeleiteten Namen; bei einer Kollision
		// koennte ein fremdes Binding mitkommen.
		if list.Items[i].Spec.InstanceID != instanceID {
			continue
		}
		b, err := s.specToBinding(ctx, &list.Items[i].Spec)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// --- Credentials-Secret ---

func (s *CRDStateStore) writeCredentialsSecret(ctx context.Context, b *Binding, name string) error {
	payload, err := json.Marshal(b.Credentials)
	if err != nil {
		return fmt.Errorf("marshal credentials for binding %q: %w", b.ID, err)
	}

	// OwnerReference zusaetzlich zum expliziten Loeschen: falls jemand das
	// Binding-CR direkt entfernt, raeumt Kubernetes das Secret mit ab.
	var owner osbv1.OSBServiceBinding
	if err := s.client.Get(ctx, s.key(b.ID), &owner); err != nil {
		return fmt.Errorf("get binding %q for owner reference: %w", b.ID, err)
	}
	ownerRef := metav1.OwnerReference{
		APIVersion: osbv1.GroupVersion.String(),
		Kind:       "OSBServiceBinding",
		Name:       owner.Name,
		UID:        owner.UID,
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var sec corev1.Secret
		err := s.client.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: name}, &sec)
		switch {
		case apierrors.IsNotFound(err):
			sec = corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            name,
					Namespace:       s.namespace,
					Labels:          map[string]string{managedByLabel: managedByValue},
					OwnerReferences: []metav1.OwnerReference{ownerRef},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{credentialsKey: payload},
			}
			return s.client.Create(ctx, &sec)
		case err != nil:
			return fmt.Errorf("get credentials secret %q: %w", name, err)
		default:
			sec.Data = map[string][]byte{credentialsKey: payload}
			sec.OwnerReferences = []metav1.OwnerReference{ownerRef}
			return s.client.Update(ctx, &sec)
		}
	})
}

func (s *CRDStateStore) deleteCredentialsSecret(ctx context.Context, name string) error {
	sec := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace}}
	if err := s.client.Delete(ctx, &sec); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete credentials secret %q: %w", name, err)
	}
	return nil
}

func (s *CRDStateStore) readCredentials(ctx context.Context, name string) (map[string]interface{}, error) {
	if name == "" {
		return nil, nil
	}
	var sec corev1.Secret
	if err := s.client.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: name}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			// Das Binding existiert, sein Secret nicht mehr. Kein Fehler:
			// ein Bind ohne Credentials ist immer noch ein Bind, und ein
			// harter Fehler machte das Binding unloeschbar.
			return nil, nil
		}
		return nil, fmt.Errorf("read credentials secret %q: %w", name, err)
	}
	raw, ok := sec.Data[credentialsKey]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var creds map[string]interface{}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, fmt.Errorf("decode credentials secret %q: %w", name, err)
	}
	return creds, nil
}

// --- Umwandlung zwischen Broker- und API-Typen ---

func toRaw(v interface{}) (*runtime.RawExtension, error) {
	switch t := v.(type) {
	case map[string]interface{}:
		if len(t) == 0 {
			return nil, nil
		}
	case []interface{}:
		if len(t) == 0 {
			return nil, nil
		}
	case nil:
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return &runtime.RawExtension{Raw: raw}, nil
}

func rawToMap(r *runtime.RawExtension) (map[string]interface{}, error) {
	if r == nil || len(r.Raw) == 0 {
		return nil, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(r.Raw, &out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

func rawToSlice(r *runtime.RawExtension) ([]interface{}, error) {
	if r == nil || len(r.Raw) == 0 {
		return nil, nil
	}
	var out []interface{}
	if err := json.Unmarshal(r.Raw, &out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

func toAPIContext(c Context) osbv1.OSBContext {
	return osbv1.OSBContext{
		Platform:            c.Platform,
		OrganizationGUID:    c.OrganizationGUID,
		SpaceGUID:           c.SpaceGUID,
		ClusterID:           c.ClusterID,
		Namespace:           c.Namespace,
		OriginatingIdentity: c.OriginatingIdentity,
	}
}

func fromAPIContext(c osbv1.OSBContext) Context {
	return Context{
		Platform:            c.Platform,
		OrganizationGUID:    c.OrganizationGUID,
		SpaceGUID:           c.SpaceGUID,
		ClusterID:           c.ClusterID,
		Namespace:           c.Namespace,
		OriginatingIdentity: c.OriginatingIdentity,
	}
}

func instanceToSpec(i *Instance) (osbv1.OSBServiceInstanceSpec, error) {
	params, err := toRaw(i.Parameters)
	if err != nil {
		return osbv1.OSBServiceInstanceSpec{}, fmt.Errorf("instance %q parameters: %w", i.ID, err)
	}
	spec := osbv1.OSBServiceInstanceSpec{
		ID: i.ID, ServiceID: i.ServiceID, PlanID: i.PlanID,
		Context:        toAPIContext(i.Context),
		Namespace:      i.Namespace,
		DashboardURL:   i.DashboardURL,
		Ready:          i.Ready,
		Parameters:     params,
		AppliedObjects: deepCopyStrings(i.AppliedObjects),
	}
	if i.AppliedRefs != nil {
		spec.AppliedRefs = make([]osbv1.AppliedObjectRef, len(i.AppliedRefs))
		for n, r := range i.AppliedRefs {
			spec.AppliedRefs[n] = osbv1.AppliedObjectRef{
				APIVersion: r.APIVersion, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name}
		}
	}
	return spec, nil
}

func specToInstance(spec *osbv1.OSBServiceInstanceSpec) (*Instance, error) {
	params, err := rawToMap(spec.Parameters)
	if err != nil {
		return nil, fmt.Errorf("instance %q parameters: %w", spec.ID, err)
	}
	out := &Instance{
		ID: spec.ID, ServiceID: spec.ServiceID, PlanID: spec.PlanID,
		Context:        fromAPIContext(spec.Context),
		Namespace:      spec.Namespace,
		Parameters:     params,
		DashboardURL:   spec.DashboardURL,
		Ready:          spec.Ready,
		AppliedObjects: deepCopyStrings(spec.AppliedObjects),
	}
	if spec.AppliedRefs != nil {
		out.AppliedRefs = make([]AppliedObjectRef, len(spec.AppliedRefs))
		for n, r := range spec.AppliedRefs {
			out.AppliedRefs[n] = AppliedObjectRef{
				APIVersion: r.APIVersion, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name}
		}
	}
	return out, nil
}

func bindingToSpec(b *Binding) (osbv1.OSBServiceBindingSpec, error) {
	params, err := toRaw(b.Parameters)
	if err != nil {
		return osbv1.OSBServiceBindingSpec{}, fmt.Errorf("binding %q parameters: %w", b.ID, err)
	}
	mounts, err := toRaw(b.VolumeMounts)
	if err != nil {
		return osbv1.OSBServiceBindingSpec{}, fmt.Errorf("binding %q volume mounts: %w", b.ID, err)
	}
	return osbv1.OSBServiceBindingSpec{
		ID: b.ID, InstanceID: b.InstanceID, ServiceID: b.ServiceID, PlanID: b.PlanID,
		AppGUID:         b.AppGUID,
		Context:         toAPIContext(b.Context),
		Ready:           b.Ready,
		Parameters:      params,
		VolumeMounts:    mounts,
		SyslogDrainURL:  b.SyslogDrainURL,
		RouteServiceURL: b.RouteServiceURL,
	}, nil
}

func (s *CRDStateStore) specToBinding(ctx context.Context, spec *osbv1.OSBServiceBindingSpec) (*Binding, error) {
	params, err := rawToMap(spec.Parameters)
	if err != nil {
		return nil, fmt.Errorf("binding %q parameters: %w", spec.ID, err)
	}
	mounts, err := rawToSlice(spec.VolumeMounts)
	if err != nil {
		return nil, fmt.Errorf("binding %q volume mounts: %w", spec.ID, err)
	}
	creds, err := s.readCredentials(ctx, spec.CredentialsSecret)
	if err != nil {
		return nil, err
	}
	return &Binding{
		ID: spec.ID, InstanceID: spec.InstanceID, ServiceID: spec.ServiceID, PlanID: spec.PlanID,
		AppGUID:         spec.AppGUID,
		Context:         fromAPIContext(spec.Context),
		Parameters:      params,
		Credentials:     creds,
		SyslogDrainURL:  spec.SyslogDrainURL,
		RouteServiceURL: spec.RouteServiceURL,
		VolumeMounts:    mounts,
		Ready:           spec.Ready,
	}, nil
}
