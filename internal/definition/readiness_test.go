package definition

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func statusCR(conditions map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{}
	conds := []interface{}{}
	for t, s := range conditions {
		conds = append(conds, map[string]interface{}{"type": t, "status": s})
	}
	status := map[string]interface{}{"conditions": conds}
	if err := unstructured.SetNestedField(u.Object, status, "status"); err != nil {
		panic(err)
	}
	return u
}

func TestEvaluateReadiness_ReadyTrue(t *testing.T) {
	sd := testDefinition(t)
	cr := statusCR(map[string]string{"Ready": "True"})

	done, _, err := EvaluateReadiness(sd, cr)
	require.NoError(t, err)
	assert.True(t, done)
}

func TestEvaluateReadiness_NotYetReady(t *testing.T) {
	sd := testDefinition(t)
	cr := statusCR(map[string]string{"Ready": "False"})

	done, reason, err := EvaluateReadiness(sd, cr)
	require.NoError(t, err)
	assert.False(t, done)
	assert.Contains(t, reason, `steht auf "False"`)
}

func TestEvaluateReadiness_ConditionMissing(t *testing.T) {
	sd := testDefinition(t)
	cr := statusCR(map[string]string{})

	done, _, err := EvaluateReadiness(sd, cr)
	require.NoError(t, err)
	assert.False(t, done)
}

func TestEvaluateReadiness_FalscherConditionNameNenntDieEchten(t *testing.T) {
	// Der Fall, der lange unsichtbar war: die Definition sucht "Ready", der
	// Operator veroeffentlicht andere Namen. Ein leerer gjson-Treffer heisst
	// "noch nicht bereit" - der Broker wartet bis in das Zeitlimit der
	// Plattform, ohne dass jemand den Grund erfaehrt. Also muss der Grund die
	// Namen nennen, die wirklich da sind.
	sd := testDefinition(t)
	cr := statusCR(map[string]string{
		"AllReplicasReady": "True",
		"ClusterAvailable": "True",
	})

	done, reason, err := EvaluateReadiness(sd, cr)

	require.NoError(t, err)
	assert.False(t, done)
	assert.Contains(t, reason, "findet im Status nichts")
	assert.Contains(t, reason, "AllReplicasReady")
	assert.Contains(t, reason, "ClusterAvailable")
}

func TestEvaluateReadiness_OhneStatusIstKeinKonfigurationsfehler(t *testing.T) {
	// Ein frisch angelegtes CR hat noch gar keinen Status. Das ist der
	// Normalfall der ersten Sekunden und darf nicht wie ein Tippfehler in der
	// Definition klingen.
	sd := testDefinition(t)
	cr := &unstructured.Unstructured{Object: map[string]interface{}{}}

	done, reason, err := EvaluateReadiness(sd, cr)

	require.NoError(t, err)
	assert.False(t, done)
	assert.Contains(t, reason, "noch keinen Status")
	assert.NotContains(t, reason, "findet im Status nichts")
}

func TestExtractCredentials_AllKeysAndFilter(t *testing.T) {
	data := map[string][]byte{
		"username": []byte("u1"),
		"password": []byte("p1"),
		"host":     []byte("h1"),
		"internal": []byte("secret-internal"),
	}

	all := ExtractCredentials(data, nil)
	assert.Equal(t, "u1", all["username"])
	assert.Equal(t, "p1", all["password"])
	assert.Equal(t, "h1", all["host"])
	assert.Equal(t, "secret-internal", all["internal"])

	filtered := ExtractCredentials(data, []string{"username", "password"})
	_, hasHost := filtered["host"]
	assert.False(t, hasHost)
	assert.Equal(t, 2, len(filtered))
}
