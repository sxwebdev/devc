package services

import (
	"sort"
	"testing"
)

func TestNamesSorted(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("catalog is empty")
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("Names() not sorted: %v", names)
	}
	for _, n := range names {
		if !Has(n) {
			t.Errorf("Names() returned %q but Has(%q) is false", n, n)
		}
	}
}

func TestTemplatePostgres(t *testing.T) {
	tmpl, ok := Template("postgres")
	if !ok {
		t.Fatal("postgres not in catalog")
	}
	if !tmpl.Enabled {
		t.Error("template must be Enabled so 'devc up' starts it")
	}
	if tmpl.Image != "postgres:18" {
		t.Errorf("image = %q, want postgres:18", tmpl.Image)
	}
	if tmpl.ContainerPort != 5432 || tmpl.HostPort != 54321 {
		t.Errorf("ports = %d/%d, want 5432/54321", tmpl.ContainerPort, tmpl.HostPort)
	}
	if tmpl.Env["POSTGRES_USER"] != "app" || tmpl.Env["POSTGRES_DB"] != "app" {
		t.Errorf("env = %v", tmpl.Env)
	}
	if len(tmpl.Volumes) != 1 || tmpl.Volumes[0].Name != "postgres-data" {
		t.Errorf("volumes = %v", tmpl.Volumes)
	}
}

func TestTemplateUnknown(t *testing.T) {
	if _, ok := Template("bogus"); ok {
		t.Error("Template(\"bogus\") should return ok=false")
	}
	if Has("bogus") {
		t.Error("Has(\"bogus\") should be false")
	}
}

// TestTemplateReturnsIndependentCopies guards the deep-copy contract: callers
// mutate the returned Env/Volumes, so a second Template call must be pristine.
func TestTemplateReturnsIndependentCopies(t *testing.T) {
	a, _ := Template("postgres")
	a.Env["POSTGRES_USER"] = "mutated"
	a.Image = "mutated"
	a.Volumes[0].Name = "mutated"

	b, _ := Template("postgres")
	if b.Env["POSTGRES_USER"] != "app" {
		t.Errorf("Env leaked across calls: %q", b.Env["POSTGRES_USER"])
	}
	if b.Image != "postgres:18" {
		t.Errorf("Image leaked across calls: %q", b.Image)
	}
	if b.Volumes[0].Name != "postgres-data" {
		t.Errorf("Volumes leaked across calls: %q", b.Volumes[0].Name)
	}
}

// TestAllTemplatesWellFormed sanity-checks every catalog entry so a new entry
// with a missing image/port is caught.
func TestAllTemplatesWellFormed(t *testing.T) {
	for _, name := range Names() {
		tmpl, ok := Template(name)
		if !ok {
			t.Errorf("%s: Has true but Template false", name)
			continue
		}
		if !tmpl.Enabled {
			t.Errorf("%s: not Enabled", name)
		}
		if tmpl.Image == "" {
			t.Errorf("%s: empty image", name)
		}
		if tmpl.ContainerPort == 0 {
			t.Errorf("%s: zero containerPort", name)
		}
		if tmpl.HostIP != "127.0.0.1" {
			t.Errorf("%s: hostIP = %q, want 127.0.0.1", name, tmpl.HostIP)
		}
	}
}

// TestNoHostPortCollision ensures published host ports are unique across the
// catalog so two services can run at once.
func TestNoHostPortCollision(t *testing.T) {
	seen := make(map[int]string)
	for _, name := range Names() {
		tmpl, _ := Template(name)
		if tmpl.HostPort == 0 {
			continue
		}
		if other, dup := seen[tmpl.HostPort]; dup {
			t.Errorf("host port %d shared by %s and %s", tmpl.HostPort, other, name)
		}
		seen[tmpl.HostPort] = name
	}
}
