package definition

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `cf delete-service` loeschte die Backing-Ressource sofort. Fuer einen
// Entwicklungsplan ist das richtig; fuer einen Produktionsplan ist es die
// unwiderrufliche Loeschung einer Datenbank auf einen Tastendruck, ohne
// Schonfrist und ohne Rueckfrage - OSB kennt fuer das Deprovision keinen Weg,
// eine Bestaetigung zu uebermitteln.
//
// `retainOnDeprovision` je Plan trennt beides: der Broker gibt die Instanz auf
// - der Datensatz verschwindet, das Deprovision ist 200, ein zweites 410 -,
// laesst die Ressourcen des Operators aber stehen und markiert sie, damit ein
// Betreiber sie findet.

const retainPlanID = "plan-retain-0000-0000-000000000001"

// engineWithRetainPlan haengt dem CNPG-Testdefinition einen Plan an, der
// zurueckhaelt.
func engineWithRetainPlan(t *testing.T) (*Engine, *OperatorClient, *multiDocRegistry, *ServiceDefinition) {
	t.Helper()
	oc, _ := newTestOperatorClient(t)
	sd := testDefinition(t)
	sd.Spec.Offering.Plans = append(sd.Spec.Offering.Plans, Plan{
		ID:                  retainPlanID,
		Name:                "prod",
		Params:              map[string]interface{}{"storageSize": "10Gi", "instances": 3},
		RetainOnDeprovision: true,
	})
	e := NewEngine(oc, sd)
	reg := newMultiDocRegistry()
	e.SetInstanceRegistry(reg)
	return e, oc, reg, sd
}

func crExists(t *testing.T, oc *OperatorClient, sd *ServiceDefinition, ns, name string) bool {
	t.Helper()
	_, err := oc.GetCR(context.Background(), sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, ns, name)
	return err == nil
}

func TestRetain_PlanOhneSchutzLoeschtWieBisher(t *testing.T) {
	e, oc, reg, sd := engineWithRetainPlan(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-del", "default", planSmall, nil))
	require.True(t, crExists(t, oc, sd, "default", "osb-inst-del"))

	require.NoError(t, e.DeprovisionInstance(ctx, sd, "default", "inst-del"))

	assert.False(t, crExists(t, oc, sd, "default", "osb-inst-del"),
		"ohne Schutz bleibt nichts stehen - das ist die Gegenprobe zum Schutz")
	_, err := reg.GetInstance(ctx, "inst-del")
	assert.Error(t, err)
}

func TestRetain_GeschuetzterPlanLaesstDieRessourceStehen(t *testing.T) {
	e, oc, reg, sd := engineWithRetainPlan(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-keep", "default", retainPlanID, nil))
	require.NoError(t, e.DeprovisionInstance(ctx, sd, "default", "inst-keep"))

	assert.True(t, crExists(t, oc, sd, "default", "osb-inst-keep"),
		"der geschuetzte Plan darf die Datenbank nicht loeschen")

	// Der Broker gibt die Instanz trotzdem auf: OSB kennt kein "teilweise
	// geloescht", und die Plattform muss das Deprovision abschliessen koennen.
	_, err := reg.GetInstance(ctx, "inst-keep")
	assert.Error(t, err, "der Datensatz verschwindet auch beim geschuetzten Plan")
}

// Eine stehengelassene Ressource, die niemand zuordnen kann, ist Muell. Sie
// muss sagen, zu welcher Instanz sie gehoerte und wann sie aufgegeben wurde.
func TestRetain_StehengelasseneRessourceIstMarkiert(t *testing.T) {
	e, oc, _, sd := engineWithRetainPlan(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-mark", "default", retainPlanID, nil))
	require.NoError(t, e.DeprovisionInstance(ctx, sd, "default", "inst-mark"))

	cr, err := oc.GetCR(ctx, sd.Spec.Provision.APIVersion, sd.Spec.Provision.Kind, "default", "osb-inst-mark")
	require.NoError(t, err)
	labels := cr.GetLabels()

	assert.Equal(t, "inst-mark", labels[LabelRetainedInstance],
		"ohne die Instanz-ID laesst sich die Ressource nicht zuordnen")
	require.NotEmpty(t, labels[LabelRetainedAt], "ohne Zeitstempel weiss niemand, seit wann sie liegt")
	_, err = time.Parse("2006-01-02T15-04-05Z", labels[LabelRetainedAt])
	assert.NoError(t, err, "der Zeitstempel muss ein Label-Wert und lesbar sein: %q", labels[LabelRetainedAt])
}

// Ohne Datensatz ist der Plan nicht bekannt. Dann muss der Broker loeschen -
// zurueckhalten waere eine Annahme ueber einen Plan, den er nicht kennt, und
// jede verlorene Buchfuehrung wuerde stillschweigend Ressourcen anhaeufen.
func TestRetain_OhneDatensatzWirdGeloescht(t *testing.T) {
	e, oc, _, sd := engineWithRetainPlan(t)
	ctx := context.Background()

	require.NoError(t, e.ProvisionInstance(ctx, cnpgServiceID, "inst-norec", "default", retainPlanID, nil))
	require.NoError(t, e.reg.DeleteInstance(ctx, "inst-norec"))

	require.NoError(t, e.DeprovisionInstance(ctx, sd, "default", "inst-norec"))

	assert.False(t, crExists(t, oc, sd, "default", "osb-inst-norec"))
}
