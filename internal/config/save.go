package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sxwebdev/devc/pkg/types"
)

// FindDevcontainerPath returns the path to the existing devcontainer.json,
// or the default path if none exists.
func FindDevcontainerPath(workspaceFolder string) string {
	paths := []string{
		filepath.Join(workspaceFolder, ".devcontainer", "devcontainer.json"),
		filepath.Join(workspaceFolder, ".devcontainer.json"),
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return paths[0] // default location
}

// ToRawMap renders a typed value into a generic map[string]any via a JSON
// round-trip. It is the single place that converts typed config structs into
// the untyped devcontainer tree (dropping zero-valued omitempty fields), so
// init, config set, and the service command all splice structs in the same way.
func ToRawMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ApplyDevcCustomization writes the typed customization into the config's
// customizations.devc block and saves it to path. Shared by `config set` and
// the `service` command so the write-back path lives in one place.
func ApplyDevcCustomization(path string, devCfg *types.DevContainerConfig, custom *types.DevcCustomization) error {
	if devCfg.Customizations == nil {
		devCfg.Customizations = make(map[string]any)
	}
	customMap, err := ToRawMap(custom)
	if err != nil {
		return fmt.Errorf("converting customization: %w", err)
	}
	devCfg.Customizations["devc"] = customMap
	return SaveDevcontainerConfig(path, devCfg)
}

// SaveDevcontainerConfig writes a DevContainerConfig to disk as JSON.
func SaveDevcontainerConfig(path string, cfg *types.DevContainerConfig) error {
	// Read existing raw JSON to preserve fields we don't model
	existing := make(map[string]any)
	if data, readErr := os.ReadFile(path); readErr == nil {
		if unmarshalErr := json.Unmarshal(data, &existing); unmarshalErr != nil {
			return fmt.Errorf("parsing existing config: %w", unmarshalErr)
		}
	}

	// Marshal the typed config and merge on top of existing
	typed, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	var updated map[string]any
	if err := json.Unmarshal(typed, &updated); err != nil {
		return err
	}

	// Merge: typed fields overwrite existing, but existing fields not in typed are preserved
	for k, v := range updated {
		if v != nil {
			existing[k] = v
		}
	}

	// Remove nil values that clutter the output, but preserve "features" entries
	// since empty maps are valid there (meaning "use defaults")
	cleanMap(existing, "features")

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// cleanMap removes nil values and empty maps from a JSON-like map.
// preserveKeys are map keys whose children should not be pruned when empty.
func cleanMap(m map[string]any, preserveKeys ...string) {
	preserve := make(map[string]bool)
	for _, k := range preserveKeys {
		preserve[k] = true
	}

	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			if preserve[k] {
				continue
			}
			cleanMap(sub)
			if len(sub) == 0 {
				delete(m, k)
			}
		}
	}
}
