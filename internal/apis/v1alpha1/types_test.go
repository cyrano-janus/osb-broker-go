package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToScheme_RegistriertAlleTypen(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, AddToScheme(s))

	// Ohne diese Registrierung liefert der Client "no kind is registered"
	// erst zur Laufzeit im Cluster - hier faellt es beim Test auf.
	for _, obj := range []runtime.Object{
		&OSBServiceInstance{}, &OSBServiceInstanceList{},
		&OSBServiceBinding{}, &OSBServiceBindingList{},
	} {
		gvks, _, err := s.ObjectKinds(obj)
		require.NoError(t, err)
		require.NotEmpty(t, gvks)
		assert.Equal(t, GroupName, gvks[0].Group)
		assert.Equal(t, Version, gvks[0].Version)
	}
}

func TestDeepCopy_IstVollstaendigEntkoppelt(t *testing.T) {
	// Die DeepCopy-Methoden sind handgeschrieben. Ein vergessenes Feld faellt
	// nicht beim Kompilieren auf, sondern erst als geteilter Zustand zwischen
	// Cache und Aufrufer - deshalb dieser Test statt Sorgfalt.
	in := &OSBServiceInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "inst", Labels: map[string]string{"a": "b"}},
		Spec: OSBServiceInstanceSpec{
			ID: "inst-1", ServiceID: "svc", PlanID: "plan",
			Context:        OSBContext{Platform: "cloudfoundry", SpaceGUID: "space-1"},
			Parameters:     &runtime.RawExtension{Raw: []byte(`{"foo":"bar"}`)},
			AppliedObjects: []string{"objekt-1"},
			AppliedRefs:    []AppliedObjectRef{{APIVersion: "v1", Kind: "Cluster", Name: "c1"}},
		},
	}
	out := in.DeepCopy()

	in.ObjectMeta.Labels["a"] = "geaendert"
	in.Spec.AppliedObjects[0] = "geaendert"
	in.Spec.AppliedRefs[0].Name = "geaendert"
	in.Spec.Parameters.Raw[2] = 'X'

	assert.Equal(t, "b", out.ObjectMeta.Labels["a"])
	assert.Equal(t, "objekt-1", out.Spec.AppliedObjects[0])
	assert.Equal(t, "c1", out.Spec.AppliedRefs[0].Name)
	assert.JSONEq(t, `{"foo":"bar"}`, string(out.Spec.Parameters.Raw))
}

func TestBindingDeepCopy_IstVollstaendigEntkoppelt(t *testing.T) {
	in := &OSBServiceBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bind", Labels: map[string]string{"x": "y"}},
		Spec: OSBServiceBindingSpec{
			ID: "bind-1", InstanceID: "inst-1",
			Parameters:        &runtime.RawExtension{Raw: []byte(`{"n":1}`)},
			VolumeMounts:      &runtime.RawExtension{Raw: []byte(`[{"m":"/data"}]`)},
			CredentialsSecret: "bind-1-credentials",
		},
	}
	out := in.DeepCopy()

	in.ObjectMeta.Labels["x"] = "geaendert"
	in.Spec.Parameters.Raw[1] = 'X'
	in.Spec.VolumeMounts.Raw[2] = 'X'

	assert.Equal(t, "y", out.ObjectMeta.Labels["x"])
	assert.JSONEq(t, `{"n":1}`, string(out.Spec.Parameters.Raw))
	assert.JSONEq(t, `[{"m":"/data"}]`, string(out.Spec.VolumeMounts.Raw))
}

func TestInstanz_SerialisiertMitStabilenFeldnamen(t *testing.T) {
	// Die Feldnamen sind das Speicherformat: wer sie aendert, macht
	// bestehende Objekte im Cluster unlesbar.
	in := &OSBServiceInstance{Spec: OSBServiceInstanceSpec{
		ID: "inst-1", ServiceID: "svc-1", PlanID: "plan-1", Ready: true,
		Context: OSBContext{SpaceGUID: "space-1"},
	}}
	raw, err := json.Marshal(in.Spec)
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "inst-1", got["id"])
	assert.Equal(t, "svc-1", got["serviceId"])
	assert.Equal(t, "plan-1", got["planId"])
	assert.Equal(t, true, got["ready"])
	assert.Equal(t, "space-1", got["context"].(map[string]interface{})["spaceGuid"])
}
