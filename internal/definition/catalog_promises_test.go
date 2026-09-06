package definition

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Katalog ist das Einzige, was ein Marktplatz vom Broker sieht, bevor er
// ihn benutzt. Sagt er weniger zu, als der Broker kann, bleibt die Faehigkeit
// ungenutzt - die Plattform lehnt ab, bevor der Broker gefragt wird. Sagt er
// mehr zu, als der Broker haelt, scheitert es beim Anwender.
//
// Diese Datei prueft beide Richtungen fuer die Zusagen, die OSB 2.17 im
// Katalog vorsieht.

func boolPtr(b bool) *bool { return &b }

// catalogJSON liefert den Katalog so, wie er ueber die Leitung geht. Ein Feld,
// das der Go-Typ traegt, aber `omitempty` verschluckt, existiert fuer die
// Plattform nicht.
func catalogJSON(t *testing.T, sd *ServiceDefinition) map[string]interface{} {
	t.Helper()
	entries := NewEngine(nil, sd).Catalog()
	require.Len(t, entries, 1)
	raw, err := json.Marshal(entries[0])
	require.NoError(t, err)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func plansJSON(t *testing.T, sd *ServiceDefinition) []map[string]interface{} {
	t.Helper()
	raw, ok := catalogJSON(t, sd)["plans"].([]interface{})
	require.True(t, ok)
	out := make([]map[string]interface{}, 0, len(raw))
	for _, p := range raw {
		out = append(out, p.(map[string]interface{}))
	}
	return out
}

func firstPlanJSON(t *testing.T, sd *ServiceDefinition) map[string]interface{} {
	t.Helper()
	plans, ok := catalogJSON(t, sd)["plans"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, plans)
	return plans[0].(map[string]interface{})
}

// --- free ---------------------------------------------------------------
//
// OSB 2.17: fehlt `free`, gilt `true`. Ein kostenpflichtiger Plan, dessen
// Angabe unterwegs verlorengeht, bewirbt sich also als kostenlos - genau die
// Falschaussage, die niemandem auffaellt, bis die Rechnung kommt.

func TestKatalog_KostenpflichtigerPlanIstNichtKostenlos(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.Plans[0].Free = boolPtr(false)

	plan := firstPlanJSON(t, sd)

	require.Contains(t, plan, "free", "free darf nicht weggelassen werden - fehlt es, gilt true")
	assert.Equal(t, false, plan["free"])
}

func TestKatalog_PlanOhneAngabeIstKostenlos(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.Plans[0].Free = nil

	assert.Equal(t, true, firstPlanJSON(t, sd)["free"],
		"ohne Angabe gilt der OSB-Standard, und er steht ausdruecklich im Katalog")
}

// --- plan_updateable ----------------------------------------------------
//
// Der Broker kann den Plan wechseln: UpdateServiceInstance nimmt ein neues
// plan_id entgegen und rendert das Manifest neu. Ob der Operator das mitmacht,
// weiss nur die Definition - CNPG kann Speicher wachsen lassen, nicht
// schrumpfen. Die Zusage gehoert deshalb an die Definition und gilt ohne
// Angabe als nicht zugesagt.

func TestKatalog_PlanwechselWirdOhneAngabeNichtZugesagt(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.PlanUpdateable = nil

	assert.Equal(t, false, catalogJSON(t, sd)["plan_updateable"],
		"was der Operator nicht nachweislich kann, darf der Katalog nicht zusagen")
}

func TestKatalog_ZugesagterPlanwechselStehtImKatalog(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.PlanUpdateable = boolPtr(true)

	assert.Equal(t, true, catalogJSON(t, sd)["plan_updateable"],
		"ohne die Zusage lehnt CF den Planwechsel ab, bevor der Broker gefragt wird")
}

// OSB 2.17 fuehrt plan_updateable auch am Plan, und dort ueberschreibt es das
// Angebot: "If specificed, this takes precedence over the Service Offering's
// plan_updateable field." Massgeblich ist dabei der Plan, auf dem die Instanz
// HEUTE liegt - "the Platform MAY request a Service Plan change on a Service
// Instance using the given Service Plan".
//
// Damit hat die Zusage eine Richtung, die das Angebot allein nicht ausdruecken
// kann: heraus aus small ja, heraus aus large nie.

func TestKatalog_PlanErbtDieZusageDesAngebots(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.PlanUpdateable = boolPtr(true)

	for _, plan := range plansJSON(t, sd) {
		assert.Equal(t, true, plan["plan_updateable"],
			"ohne eigene Angabe gilt am Plan, was das Angebot sagt")
	}
}

func TestKatalog_PlanUeberschreibtDieZusageDesAngebots(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.PlanUpdateable = boolPtr(true)
	sd.Spec.Offering.Plans[1].PlanUpdateable = boolPtr(false)

	plans := plansJSON(t, sd)
	assert.Equal(t, true, plans[0]["plan_updateable"], "aus small heraus bleibt der Wechsel zugesagt")
	assert.Equal(t, false, plans[1]["plan_updateable"],
		"aus large heraus nicht: dort schrumpft der Speicher nicht, und der Loeschschutz ginge verloren")
}

func TestKatalog_PlanKannDenWechselAlleinZusagen(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.PlanUpdateable = nil
	sd.Spec.Offering.Plans[0].PlanUpdateable = boolPtr(true)

	assert.Equal(t, true, plansJSON(t, sd)[0]["plan_updateable"],
		"ein Plan darf zusagen, ohne dass das Angebot es tut")
}

// Die Sicherungslinie: eine Plattform, die den Plan-Vorrang NICHT umsetzt,
// liest die Zusage am Angebot. Stuende dort `true`, waehrend ein Plan sie
// zurueckzieht, erlaubte diese Plattform genau den Wechsel, den der Plan
// verbietet. Das Angebot sagt deshalb nur zu, was JEDER Plan haelt.
func TestKatalog_AngebotSagtNurZuWasJederPlanHaelt(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.PlanUpdateable = boolPtr(true)
	sd.Spec.Offering.Plans[1].PlanUpdateable = boolPtr(false)

	assert.Equal(t, false, catalogJSON(t, sd)["plan_updateable"],
		"eine Plattform ohne Plan-Vorrang darf aus dieser Zusage nicht mehr ableiten, als gilt")
}

// --- die Aufloesung, die Katalog und Update-Pfad teilen ------------------

func TestPlanwechselZusage_LiestDenQuellplan(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.PlanUpdateable = boolPtr(true)
	sd.Spec.Offering.Plans[1].PlanUpdateable = boolPtr(false)

	small, large := sd.Spec.Offering.Plans[0].ID, sd.Spec.Offering.Plans[1].ID
	assert.True(t, PlanChangeAllowed(sd, small))
	assert.False(t, PlanChangeAllowed(sd, large))
}

func TestPlanwechselZusage_UnbekannterQuellplanSagtNichtsZu(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.PlanUpdateable = boolPtr(true)

	assert.False(t, PlanChangeAllowed(sd, "plan-den-es-nicht-mehr-gibt"),
		"ein Plan, der aus der Definition verschwunden ist, sagt nichts mehr zu")
}

// --- instances_retrievable / bindings_retrievable -----------------------
//
// Anders als plan_updateable sind das keine Aussagen ueber den Operator,
// sondern ueber den Broker selbst: die GET-Endpunkte sind fuer jede Definition
// registriert. Sie sind deshalb keine Definitionsfelder, sondern fest - und
// ein Test in internal/handlers haelt die Zusage gegen die Route.

func TestKatalog_AbrufbarkeitWirdAngemeldet(t *testing.T) {
	entry := catalogJSON(t, testDefinition(t))

	assert.Equal(t, true, entry["instances_retrievable"])
	assert.Equal(t, true, entry["bindings_retrievable"])
}

// --- metadata -----------------------------------------------------------
//
// Ohne metadata zeigt eine Marktplatz-Kachel den technischen Namen und sonst
// nichts. Der Inhalt ist bewusst frei: OSB schreibt keine Schluessel vor,
// verschiedene Marktplaetze lesen verschiedene.

func TestKatalog_AnzeigeblockErreichtDenMarktplatz(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.Metadata = map[string]interface{}{
		"displayName":      "PostgreSQL",
		"documentationUrl": "https://example.invalid/docs",
	}
	sd.Spec.Offering.Plans[0].Metadata = map[string]interface{}{
		"bullets": []interface{}{"1 GiB Speicher"},
	}

	entry := catalogJSON(t, sd)
	meta, ok := entry["metadata"].(map[string]interface{})
	require.True(t, ok, "der Anzeigeblock des Angebots fehlt im Katalog")
	assert.Equal(t, "PostgreSQL", meta["displayName"])
	assert.Equal(t, "https://example.invalid/docs", meta["documentationUrl"])

	planMeta, ok := firstPlanJSON(t, sd)["metadata"].(map[string]interface{})
	require.True(t, ok, "der Anzeigeblock des Plans fehlt im Katalog")
	assert.Equal(t, []interface{}{"1 GiB Speicher"}, planMeta["bullets"])
}

// Ein leerer Anzeigeblock ist keine Aussage und gehoert nicht in den Katalog -
// sonst rendert ein Marktplatz eine leere Kachel statt seines Standards.
func TestKatalog_OhneAnzeigeblockStehtKeinerDrin(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Offering.Metadata = nil

	assert.NotContains(t, catalogJSON(t, sd), "metadata")
}

// --- maximum_polling_duration -------------------------------------------
//
// Der Broker gibt die Bereitschaftspruefung nach ReadinessTimeout auf. Fragt
// die Plattform danach weiter, wartet sie auf eine Antwort, die nie kommt;
// hoert sie frueher auf, meldet sie einen Fehlschlag, den der Broker nicht
// sieht. Beide Zahlen muessen dieselbe sein, also stammen sie aus derselben
// Quelle.

func TestKatalog_PollfristFolgtDerBereitschaftsfrist(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Readiness.TimeoutSeconds = 900

	assert.Equal(t, float64(900), firstPlanJSON(t, sd)["maximum_polling_duration"])
}

func TestKatalog_PollfristGiltAuchOhneAngabe(t *testing.T) {
	sd := testDefinition(t)
	sd.Spec.Readiness.TimeoutSeconds = 0

	assert.Equal(t, float64(DefaultReadinessTimeout.Seconds()), firstPlanJSON(t, sd)["maximum_polling_duration"],
		"ohne Angabe gilt die Standardfrist, und die Plattform muss sie kennen")
}

// --- Der Waechter fuer das Angebot selbst -------------------------------
//
// Fuer Bind, CredentialMapping und Plan gibt es ihn; fuer Offering fehlte er,
// und genau dort kommen planUpdateable und metadata hinzu.

func TestSchema_OfferingDecktDenGoTyp(t *testing.T) {
	props := schemaProperties(t, loadDefinitionSchema(t), "offering")
	for _, field := range structJSONFields(t, Offering{}) {
		assert.Contains(t, props, field, "spec.offering.%s fehlt im JSON-Schema", field)
	}
}

func TestSchema_OfferingVerspricht_NichtsUnbekanntes(t *testing.T) {
	known := map[string]bool{}
	for _, field := range structJSONFields(t, Offering{}) {
		known[field] = true
	}
	for key := range schemaProperties(t, loadDefinitionSchema(t), "offering") {
		assert.True(t, known[key], "das Schema nennt offering.%s, der Go-Typ kennt es nicht", key)
	}
}
