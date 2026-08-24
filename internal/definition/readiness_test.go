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

	done, err := EvaluateReadiness(sd, cr)
	require.NoError(t, err)
	assert.True(t, done)
}

func TestEvaluateReadiness_NotYetReady(t *testing.T) {
	sd := testDefinition(t)
	cr := statusCR(map[string]string{"Ready": "False"})

	done, err := EvaluateReadiness(sd, cr)
	require.NoError(t, err)
	assert.False(t, done)
}

func TestEvaluateReadiness_ConditionMissing(t *testing.T) {
	sd := testDefinition(t)
	cr := statusCR(map[string]string{})

	done, err := EvaluateReadiness(sd, cr)
	require.NoError(t, err)
	assert.False(t, done)
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
