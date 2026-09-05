package definition

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	cnpgServiceID = "f48a9e21-cnpg-0000-0000-000000000001"
	planSmall     = "plan-small-0000-0000-000000000001"
	planLarge     = "plan-large-0000-0000-000000000002"
)

// engineWithRegistry baut eine Engine ueber der CNPG-Testdefinition mit einem
// Datensatzspeicher - ohne ihn kann ein Update weder den Plan noch die
// bisherigen Parameter kennen.
func engineWithRegistry(t *testing.T) (*Engine, *OperatorClient, *multiDocRegistry) {
	t.Helper()
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	e := NewEngine(oc, sd)
	reg := newMultiDocRegistry()
	e.SetInstanceRegistry(reg)
	return e, oc, reg
}

func storageOf(t *testing.T, oc *OperatorClient, name string) string {
	t.Helper()
	sd := testDefinition(t)
	cr, err := oc.GetCR(context.Background(),
		sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", name)
	require.NoError(t, err)
	size, found, err := unstructured.NestedString(cr.Object, "spec", "storage", "size")
	require.NoError(t, err)
	require.True(t, found, "spec.storage.size fehlt im CR")
	return size
}

func TestRenderProvision_BenutzerparameterUeberschreibtPlanwert(t *testing.T) {
	sd := testDefinition(t)
	plan, err := sd.PlanByID(planLarge)
	require.NoError(t, err)

	out, err := RenderProvision(sd, "inst-1", plan.Params,
		map[string]interface{}{"storageSize": "25Gi"})
	require.NoError(t, err)

	assert.Contains(t, out, "size: 25Gi", "der Benutzerwert muss den Planwert ersetzen")
	assert.Contains(t, out, "instances: 3", "ein nicht genannter Planwert bleibt stehen")
}

// Der Plan darf die Ueberlagerung nicht ueberleben: PlanByID gibt einen Zeiger
// in die geladene Definition zurueck, und die haelt der Prozess fuer die
// gesamte Laufzeit. Wuerde RenderProvision hineinschreiben, bekaeme die
// naechste Instanz desselben Plans den Wert des vorigen Kunden.
func TestRenderProvision_PlanBleibtUnberuehrt(t *testing.T) {
	sd := testDefinition(t)
	plan, err := sd.PlanByID(planLarge)
	require.NoError(t, err)

	_, err = RenderProvision(sd, "inst-1", plan.Params,
		map[string]interface{}{"storageSize": "25Gi"})
	require.NoError(t, err)

	assert.Equal(t, "10Gi", plan.Params["storageSize"],
		"plan.Params darf nicht beschrieben werden")

	out, err := RenderProvision(sd, "inst-2", plan.Params, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "size: 10Gi",
		"die naechste Instanz muss wieder den Planwert bekommen")
}

// Wer die Sichten trennen will, kann das: .parameters traegt ausschliesslich
// die Benutzerwerte, ohne die Plan-Vorgaben.
func TestRenderProvision_ParametersIstDieReineBenutzersicht(t *testing.T) {
	yaml := strings.Replace(validYAML,
		"size: {{ .plan.storageSize }}",
		"size: {{ .parameters.storageSize }}", 1)
	sd, err := Parse([]byte(yaml))
	require.NoError(t, err)
	plan, err := sd.PlanByID(planLarge)
	require.NoError(t, err)

	out, err := RenderProvision(sd, "inst-1", plan.Params,
		map[string]interface{}{"storageSize": "25Gi"})
	require.NoError(t, err)
	assert.Contains(t, out, "size: 25Gi")

	// Ohne den Parameter ist das ein Fehler, kein leerer Wert - es gilt
	// missingkey=error. Die Meldung muss den fehlenden Schluessel nennen und
	// nicht den Typnamen des Punktes.
	_, err = RenderProvision(sd, "inst-1", plan.Params, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storageSize")
	assert.NotContains(t, err.Error(), "TemplateData")
}

func TestEngine_Provision_ParameterErreichenCRUndDatensatz(t *testing.T) {
	e, oc, reg := engineWithRegistry(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-p", "default", planSmall,
		map[string]interface{}{"storageSize": "5Gi"}))

	assert.Equal(t, "5Gi", storageOf(t, oc, "osb-inst-p"),
		"der Benutzerparameter muss im angelegten CR stehen")

	rec, err := reg.GetInstance(ctx, "inst-p")
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"storageSize": "5Gi"}, rec.Parameters,
		"der Datensatz muss die Parameter tragen - ein PATCH schickt nur das Geaenderte")
}

// Die Whitelist gilt in der Engine selbst, nicht erst im HTTP-Handler: die
// Engine ist exportiert und darf sich nicht darauf verlassen, dass jemand
// vorher geprueft hat.
func TestEngine_Provision_UnerlaubterParameterAbgelehnt(t *testing.T) {
	e, _, _ := engineWithRegistry(t)

	err := e.ProvisionInstance(context.Background(), cnpgServiceID, "inst-bad", "default", planSmall,
		map[string]interface{}{"bogus": 1})
	require.ErrorIs(t, err, ErrParameterNotAllowed)
	assert.Contains(t, err.Error(), "bogus")
}

// plan_id ist im PATCH optional. Ohne diesen Rueckfall scheiterte jedes reine
// Parameter-Update an einem Plan mit dem Namen "".
func TestEngine_Update_OhnePlanIDBleibtDerPlanBestehen(t *testing.T) {
	e, oc, reg := engineWithRegistry(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-u", "default", planSmall, nil))

	done, err := e.UpdateInstance(ctx, cnpgServiceID, "inst-u", "default", "",
		map[string]interface{}{"storageSize": "7Gi"})
	require.NoError(t, err)
	assert.True(t, done)

	assert.Equal(t, "7Gi", storageOf(t, oc, "osb-inst-u"))
	rec, err := reg.GetInstance(ctx, "inst-u")
	require.NoError(t, err)
	assert.Equal(t, planSmall, rec.PlanID, "der Plan darf sich ohne plan_id nicht aendern")
}

func TestEngine_Update_ParameterWerdenVerschmolzen(t *testing.T) {
	e, oc, reg := engineWithRegistry(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-m", "default", planSmall,
		map[string]interface{}{"storageSize": "5Gi", "replicas": 2}))

	_, err := e.UpdateInstance(ctx, cnpgServiceID, "inst-m", "default", "",
		map[string]interface{}{"storageSize": "9Gi"})
	require.NoError(t, err)

	rec, err := reg.GetInstance(ctx, "inst-m")
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"storageSize": "9Gi", "replicas": 2}, rec.Parameters,
		"gesendete Schluessel ersetzen, ungenannte bleiben stehen")
	assert.Equal(t, "9Gi", storageOf(t, oc, "osb-inst-m"))
}

// Ein Planwechsel muss die gesamte Konfiguration im Zielplan erlauben, nicht
// nur die frisch gesendeten Schluessel: der grosse Plan fuehrt gar keine
// allowedParameters, also darf ein dorthin mitgeschleppter Parameter nicht
// stillschweigend weiterlaufen.
func TestEngine_Update_MitgeschleppterParameterImZielplanAbgelehnt(t *testing.T) {
	e, _, _ := engineWithRegistry(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-x", "default", planSmall,
		map[string]interface{}{"storageSize": "5Gi"}))

	_, err := e.UpdateInstance(ctx, cnpgServiceID, "inst-x", "default", planLarge, nil)
	require.ErrorIs(t, err, ErrParameterNotAllowed)
	assert.Contains(t, err.Error(), "storageSize")
}

// Ein Parameter, den das Template nicht liest, aendert das Manifest nicht -
// den Zustand der Instanz aber sehr wohl. Ohne das Nachfuehren des Datensatzes
// meldete GET /v2/service_instances anschliessend den alten Wert.
func TestEngine_Update_DatensatzAuchOhneManifestaenderung(t *testing.T) {
	e, oc, reg := engineWithRegistry(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-n", "default", planSmall, nil))
	sd := testDefinition(t)
	before, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-n")
	require.NoError(t, err)

	// replicas steht in allowedParameters, kommt im Template aber nicht vor.
	_, err = e.UpdateInstance(ctx, cnpgServiceID, "inst-n", "default", "",
		map[string]interface{}{"replicas": 2})
	require.NoError(t, err)

	after, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-n")
	require.NoError(t, err)
	assert.Equal(t, before.GetResourceVersion(), after.GetResourceVersion(),
		"ein unbenutzter Parameter darf den Operator nicht aufwecken")

	rec, err := reg.GetInstance(ctx, "inst-n")
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"replicas": 2}, rec.Parameters)
}

func TestParamsEqual(t *testing.T) {
	assert.True(t, ParamsEqual(nil, nil))
	assert.True(t, ParamsEqual(nil, map[string]interface{}{}))
	assert.True(t, ParamsEqual(
		map[string]interface{}{"a": 1},
		map[string]interface{}{"a": 1.0}),
		"dieselbe Zahl aus YAML und aus JSON muss gleich sein")
	assert.False(t, ParamsEqual(
		map[string]interface{}{"a": 1},
		map[string]interface{}{"a": 2}))
	assert.False(t, ParamsEqual(
		map[string]interface{}{"a": 1},
		map[string]interface{}{"a": 1, "b": 2}))
}
