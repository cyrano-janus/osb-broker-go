package definition

import (
	"strings"
	"testing"
)

func TestSanitizeInstanceName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"my-db", "osb-my-db"},
		{"930fca69-63a2-45db-abee-46770af47008", "osb-930fca69-63a2-45db-abee-46770af47008"},
		{"UPPER_case.id", "osb-upper-case-id"},
		{"-leading-dash-", "osb-leading-dash"},
	}
	for _, c := range cases {
		got := SanitizeInstanceName(c.in)
		if got != c.want {
			t.Errorf("SanitizeInstanceName(%q) = %q, want %q", c.in, got, c.want)
		}
		if len(got) > 63 {
			t.Errorf("SanitizeInstanceName(%q) too long: %d", c.in, len(got))
		}
	}

	long := strings.Repeat("x", 100) + "-930fca69-63a2-45db-abee"
	got := SanitizeInstanceName(long)
	if len(got) > 63 {
		t.Errorf("long name not truncated deterministically: %q (%d)", got, len(got))
	}
	if SanitizeInstanceName(long) != got {
		t.Error("sanitize must be deterministic")
	}
}

func TestRender_CRManifestWithPlanParams(t *testing.T) {
	sd, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	plan, err := sd.PlanByID("plan-large-0000-0000-000000000002")
	if err != nil {
		t.Fatalf("planByID: %v", err)
	}

	out, err := RenderProvision(sd, "my-instance-1", plan.Params)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"apiVersion: postgresql.cnpg.io/v1",
		"kind: Cluster",
		"name: osb-my-instance-1",
		"instances: 3",
		"size: 10Gi",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered manifest missing %q\n---\n%s", want, out)
		}
	}
}

func TestRender_InvalidTemplateRejected(t *testing.T) {
	bad := strings.Replace(validYAML,
		"{{ .plan.instances }}", "{{ .plan.instances ", 1)
	sd, err := Parse([]byte(bad))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err = RenderProvision(sd, "x", map[string]interface{}{}); err == nil {
		t.Fatal("expected template syntax error")
	}
}

func TestRender_MissingTemplateKeyFails(t *testing.T) {
	sd, err := Parse([]byte(strings.Replace(validYAML, "storageSize: 10Gi\n          instances: 3", "", 1)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// plan has no params now; rendering must fail on missing .plan.storageSize
	plan, perr := sd.PlanByID("plan-large-0000-0000-000000000002")
	if perr != nil {
		t.Fatalf("planByID: %v", perr)
	}
	if _, err := RenderProvision(sd, "x", plan.Params); err == nil {
		t.Fatal("expected missing-key error")
	}
}

const validYAML = `
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: cnpg-postgresql
spec:
  offering:
    id: f48a9e21-cnpg-0000-0000-000000000001
    name: cnpg-postgresql
    description: "CloudNativePG PostgreSQL clusters"
    plans:
      - id: plan-small-0000-0000-000000000001
        name: small
        allowedParameters: [replicas]
        params:
          storageSize: 1Gi
          instances: 1
      - id: plan-large-0000-0000-000000000002
        name: large
        description: "HA, 3 instances, 10Gi"
        params:
          storageSize: 10Gi
          instances: 3
  provision:
    apiVersion: postgresql.cnpg.io/v1
    kind: Cluster
    template: |
      apiVersion: postgresql.cnpg.io/v1
      kind: Cluster
      metadata:
        name: {{ .safeName }}
      spec:
        instances: {{ .plan.instances }}
        storage:
          size: {{ .plan.storageSize }}
  readiness:
    statusJSONPath: 'status.conditions.#(type=="Ready").status'
    expectedValue: "True"
    timeoutSeconds: 600
  bind:
    credentialsFromSecret: "{{ .safeName }}-app"
`
