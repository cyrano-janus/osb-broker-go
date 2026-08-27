package definition

import (
	"strings"
	"testing"
)

// --- Multi-Doc template rendering (4.6) ---

func TestRenderProvision_SingleDoc_Unchanged(t *testing.T) {
	// Existing single-doc templates must keep working: one rendered doc,
	// no split markers introduced.
	sd := testDefinition(t)
	sd.Spec.Provision.Template = "apiVersion: example.com/v1\nkind: Thing\nmetadata:\n  name: {{ .safeName }}\n"

	out, err := RenderProvision(sd, "inst-1", nil)
	if err != nil {
		t.Fatalf("RenderProvision: %v", err)
	}
	if strings.Contains(out, "\n---\n") || strings.HasPrefix(out, "---") {
		t.Fatalf("single doc must not be split-prefixed: %q", out)
	}
	if !strings.Contains(out, "name: osb-inst-1") {
		t.Fatalf("template not rendered: %q", out)
	}
}

func TestRenderProvision_MultiDoc_AllDocsRendered(t *testing.T) {
	// A template with multiple YAML documents (--- separated) renders
	// every document; the separator stays intact for downstream splitting.
	sd := testDefinition(t)
	sd.Spec.Provision.Template = `apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaNodePool
metadata:
  name: {{ .safeName }}-pool
---
apiVersion: kafka.strimzi.io/v1beta2
kind: Kafka
metadata:
  name: {{ .safeName }}
`

	out, err := RenderProvision(sd, "inst-1", nil)
	if err != nil {
		t.Fatalf("RenderProvision: %v", err)
	}
	if got := strings.Count(out, "\n---"); got != 1 {
		t.Fatalf("expected 1 doc separator between 2 docs, got %d in %q", got, out)
	}
	if !strings.Contains(out, "name: osb-inst-1-pool") || !strings.Contains(out, "name: osb-inst-1\n") {
		t.Fatalf("not all docs rendered with safeName: %q", out)
	}
}

func TestSplitManifests_SplitsAndTrims(t *testing.T) {
	in := "a: 1\n---\nb: 2\n---\nc: 3"
	docs := SplitManifests(in)
	if len(docs) != 3 {
		t.Fatalf("expected 3 docs, got %d: %q", len(docs), docs)
	}
	for _, d := range docs {
		if d == "" || strings.HasPrefix(d, "---") {
			t.Fatalf("doc must be non-empty and unprefixed: %q", d)
		}
	}
}

func TestSplitManifests_EmptyDocsDropped(t *testing.T) {
	// Leading separators and blank documents (common when authors format
	// templates with stray ---) are dropped.
	in := "---\na: 1\n---\n\n---\nb: 2\n---\n"
	docs := SplitManifests(in)
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d: %q", len(docs), docs)
	}
}

func TestSplitManifests_SingleDocPassthrough(t *testing.T) {
	docs := SplitManifests("apiVersion: v1\nkind: X")
	if len(docs) != 1 || !strings.Contains(docs[0], "kind: X") {
		t.Fatalf("single doc passthrough broken: %q", docs)
	}
}

func TestValidate_MultiDocTemplate_Accepted(t *testing.T) {
	// Validation must accept multi-doc templates (they contain ---).
	yaml := `apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: md-test
spec:
  offering:
    id: md-0000
    name: md-test
    description: multi-doc test service
    tags: [test]
    plans:
      - id: plan-md-small
        name: small
        description: small
        params:
          size: 1Gi
  provision:
    apiVersion: example.com/v1
    kind: Composite
    template: |
      apiVersion: example.com/v1
      kind: Alpha
      metadata:
        name: {{ .safeName }}
      ---
      apiVersion: example.com/v1
      kind: Beta
      metadata:
        name: {{ .safeName }}-b
  readiness:
    statusJSONPath: '.conditions.#(type=="Ready").status'
    expectedValue: "True"
  bind:
    credentialsFromSecret: "{{ .safeName }}-app"
`
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Fatalf("multi-doc definition must parse: %v", err)
	}
}
