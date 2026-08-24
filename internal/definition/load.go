package definition

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadFromDir reads every *.yaml / *.yml file in dir and parses it as a
// ServiceDefinition. Missing dir yields an empty catalog (no error) so the
// broker can start without definitions.
func LoadFromDir(dir string) ([]*ServiceDefinition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*ServiceDefinition
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		sd, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("definition %s: %w", name, err)
		}
		out = append(out, sd)
	}
	return out, nil
}
