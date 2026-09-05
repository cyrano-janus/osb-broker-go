package definition

import "testing"

// CatalogTest verifies every shipped definition parses and the catalog
// stays consistent — guards against definition drift breaking the build.
func TestAllShippedDefinitionsParse(t *testing.T) {
	defs, err := LoadFromDir("../../definitions")
	if err != nil {
		t.Fatalf("load shipped definitions: %v", err)
	}
	if len(defs) < 3 {
		t.Fatalf("expected >=3 shipped definitions, got %d", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Metadata.Name] = true
	}
	// Vier Definitionen liegen bewusst unter definitions/unsupported/ und
	// werden nicht geladen: Redis und Redpanda, weil ihre Lizenz die
	// Bereitstellung als managed Service untersagt, MinIO, weil das Projekt
	// aufgegeben ist, und Valkey, weil sein Operator kein Credentials-Secret
	// anlegt. Die Begruendung je Fall steht in deren README.
	for _, want := range []string{"cnpg-postgresql", "rabbitmq-cluster", "seaweedfs-s3"} {
		if !names[want] {
			t.Errorf("missing shipped definition %q (got %v)", want, names)
		}
	}
}
