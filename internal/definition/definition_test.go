package definition

import (
	"testing"
)

func TestParse_ValidDefinition(t *testing.T) {
	yaml := `
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: cnpg-postgresql
spec:
  offering:
    id: f48a9e21-cnpg-0000-0000-000000000001
    name: cnpg-postgresql
    description: "CloudNativePG PostgreSQL clusters"
    bindable: true
    tags:
      - postgresql
      - database
    plans:
      - id: plan-small-0000-0000-000000000001
        name: small
        description: "Single instance, 1Gi"
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
      metadata:
        name: {{ .instanceID }}
      spec:
        instances: {{ .plan.instances }}
        storage:
          size: {{ .plan.storageSize }}
  readiness:
    statusJSONPath: '.status.conditions[?(@.type=="Ready")].status'
    expectedValue: "True"
    timeoutSeconds: 600
  bind:
    credentialsFromSecret: "{{ .instanceID }}-app"
`
	sd, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if sd.Metadata.Name != "cnpg-postgresql" {
		t.Errorf("name = %q", sd.Metadata.Name)
	}
	if sd.Spec.Offering.ID == "" || sd.Spec.Offering.Name != "cnpg-postgresql" {
		t.Errorf("offering not parsed: %+v", sd.Spec.Offering)
	}
	if len(sd.Spec.Offering.Plans) != 2 {
		t.Fatalf("want 2 plans, got %d", len(sd.Spec.Offering.Plans))
	}
	if sd.Spec.Provision.APIVersion != "postgresql.cnpg.io/v1" {
		t.Errorf("provision apiVersion = %q", sd.Spec.Provision.APIVersion)
	}
	if sd.Spec.Provision.Kind != "Cluster" {
		t.Errorf("provision kind = %q", sd.Spec.Provision.Kind)
	}
	if sd.Spec.Readiness.StatusJSONPath == "" {
		t.Error("readiness JSONPath missing")
	}
	if sd.Spec.Bind.CredentialsFromSecret != "{{ .instanceID }}-app" {
		t.Errorf("bind secret template = %q", sd.Spec.Bind.CredentialsFromSecret)
	}
}

func TestParse_MissingOfferingIDRejected(t *testing.T) {
	yaml := `
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: broken
spec:
  offering:
    name: no-id-given
    plans:
      - id: p1
        name: small
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for missing offering.id")
	}
}

func TestParse_MissingPlansRejected(t *testing.T) {
	yaml := `
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: broken
spec:
  offering:
    id: some-id
    name: no-plans
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for missing plans")
	}
}

func TestParse_WrongKindRejected(t *testing.T) {
	yaml := `
apiVersion: broker.osb.io/v1alpha1
kind: SomethingElse
metadata:
  name: broken
spec: {}
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestParse_MissingProvisionTemplateRejected(t *testing.T) {
	yaml := `
apiVersion: broker.osb.io/v1alpha1
kind: ServiceDefinition
metadata:
  name: broken
spec:
  offering:
    id: some-id
    name: x
    plans:
      - id: p1
        name: small
  readiness:
    statusJSONPath: '.status.ready'
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing provision template")
	}
}
