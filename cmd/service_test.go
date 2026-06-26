package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sxwebdev/devc/internal/config"
)

// writeDevcontainer writes a minimal devcontainer.json into a temp workspace and
// returns the workspace folder.
func writeDevcontainer(t *testing.T, body string) string {
	t.Helper()
	ws := t.TempDir()
	dir := filepath.Join(ws, ".devcontainer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// loadServices reads back the persisted enabled services for assertions.
func loadServices(t *testing.T, ws string) map[string]string {
	t.Helper()
	devCfg, err := config.LoadDevcontainerConfig(ws)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	custom, err := config.ExtractDevcCustomization(devCfg)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	out := make(map[string]string)
	for name, svc := range custom.Services {
		out[name] = svc.Image
	}
	return out
}

func runService(args ...string) error {
	cmd := newServiceCmd()
	cmd.Writer = io.Discard
	cmd.ErrWriter = io.Discard
	// urfave/cli treats argv[0] as the program name, so prepend the command name.
	return cmd.Run(context.Background(), append([]string{"service"}, args...))
}

const emptyConfig = `{"name":"t","image":"img","customizations":{"devc":{"securityProfile":"moderate"}}}`

func TestServiceAddToNilMap(t *testing.T) {
	ws := writeDevcontainer(t, emptyConfig)
	if err := runService("add", "postgres", "--path", ws); err != nil {
		t.Fatalf("add: %v", err)
	}
	svcs := loadServices(t, ws)
	if svcs["postgres"] != "postgres:18" {
		t.Errorf("postgres = %q, want postgres:18; all=%v", svcs["postgres"], svcs)
	}
}

func TestServiceAddMultiple(t *testing.T) {
	ws := writeDevcontainer(t, emptyConfig)
	if err := runService("add", "postgres", "redis", "--path", ws); err != nil {
		t.Fatalf("add: %v", err)
	}
	svcs := loadServices(t, ws)
	if len(svcs) != 2 || svcs["redis"] != "redis:8" {
		t.Errorf("services = %v", svcs)
	}
}

func TestServiceAddDuplicateSkipped(t *testing.T) {
	ws := writeDevcontainer(t, emptyConfig)
	_ = runService("add", "postgres", "--path", ws)

	// Mutate the stored image, then add again without --force: must be preserved.
	devCfg, _ := config.LoadDevcontainerConfig(ws)
	custom, _ := config.ExtractDevcCustomization(devCfg)
	custom.Services["postgres"].Image = "custom:1"
	if err := config.ApplyDevcCustomization(config.FindDevcontainerPath(ws), devCfg, custom); err != nil {
		t.Fatal(err)
	}

	if err := runService("add", "postgres", "--path", ws); err != nil {
		t.Fatalf("add dup: %v", err)
	}
	if got := loadServices(t, ws)["postgres"]; got != "custom:1" {
		t.Errorf("duplicate add overwrote user edit: %q", got)
	}
}

func TestServiceAddForceOverwrites(t *testing.T) {
	ws := writeDevcontainer(t, emptyConfig)
	_ = runService("add", "postgres", "--path", ws)
	devCfg, _ := config.LoadDevcontainerConfig(ws)
	custom, _ := config.ExtractDevcCustomization(devCfg)
	custom.Services["postgres"].Image = "custom:1"
	_ = config.ApplyDevcCustomization(config.FindDevcontainerPath(ws), devCfg, custom)

	if err := runService("add", "postgres", "--force", "--path", ws); err != nil {
		t.Fatalf("add --force: %v", err)
	}
	if got := loadServices(t, ws)["postgres"]; got != "postgres:18" {
		t.Errorf("--force did not reset image: %q", got)
	}
}

func TestServiceAddUnknown(t *testing.T) {
	ws := writeDevcontainer(t, emptyConfig)
	if err := runService("add", "bogus", "--path", ws); err == nil {
		t.Error("expected error for unknown service")
	}
}

func TestServiceRemove(t *testing.T) {
	ws := writeDevcontainer(t, emptyConfig)
	_ = runService("add", "postgres", "redis", "--path", ws)
	if err := runService("remove", "postgres", "--path", ws); err != nil {
		t.Fatalf("remove: %v", err)
	}
	svcs := loadServices(t, ws)
	if _, ok := svcs["postgres"]; ok {
		t.Error("postgres not removed")
	}
	if _, ok := svcs["redis"]; !ok {
		t.Error("redis should remain")
	}
}

func TestServiceRemoveAllPrunesKey(t *testing.T) {
	ws := writeDevcontainer(t, emptyConfig)
	_ = runService("add", "postgres", "--path", ws)
	if err := runService("remove", "postgres", "--path", ws); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// services key should be pruned from the persisted JSON.
	data, _ := os.ReadFile(config.FindDevcontainerPath(ws))
	if strings.Contains(string(data), "\"services\"") {
		t.Errorf("services key not pruned: %s", data)
	}
}

func TestServiceRemoveNonExistent(t *testing.T) {
	ws := writeDevcontainer(t, emptyConfig)
	_ = runService("add", "redis", "--path", ws)
	// removing a service that isn't there (but others exist) → no matching → error
	if err := runService("remove", "postgres", "--path", ws); err == nil {
		t.Error("expected error removing absent service")
	}
}

func TestServiceMissingConfig(t *testing.T) {
	ws := t.TempDir() // no devcontainer.json
	if err := runService("add", "postgres", "--path", ws); err == nil {
		t.Error("expected error when no devcontainer.json present")
	}
}
