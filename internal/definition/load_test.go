package definition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromDir_ReadsYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(validYAML), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("skip me"), 0o600))

	sds, err := LoadFromDir(dir)
	require.NoError(t, err)
	assert.Len(t, sds, 1)
	assert.Equal(t, "cnpg-postgresql", sds[0].Metadata.Name)
}

func TestLoadFromDir_MissingDirIsEmpty(t *testing.T) {
	sds, err := LoadFromDir("/nonexistent/dir/xyz")
	require.NoError(t, err)
	assert.Empty(t, sds)
}

func TestLoadFromDir_InvalidDefinitionFails(t *testing.T) {
	dir := t.TempDir()
	bad := strings.Replace(validYAML, "id: f48a9e21-cnpg-0000-0000-000000000001", "id: \"\"", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(bad), 0o600))
	_, err := LoadFromDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad.yaml")
}
