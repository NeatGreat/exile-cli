package config

import (
	"os"
	"path/filepath"
	"testing"

	"exileagent/internal/container"
)

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exile", "config.json")
	t.Setenv("EXILE_CONFIG", path)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project("/work/proj")
	if p.Add(Entry{Mount: container.Mount{Source: "/work/lib", Mode: container.ReadWrite}}) {
		t.Error("Add reported a replacement for a new mount")
	}
	if !p.Add(Entry{Mount: container.Mount{Source: "/work/lib", Mode: container.ReadOnly}, Disabled: true}) {
		t.Error("Add did not replace the mount with the same source")
	}
	if !p.AllowHost("registry.npmjs.org") || p.AllowHost("registry.npmjs.org") {
		t.Error("AllowHost did not report correctly")
	}
	c.Global.Add(Entry{Mount: container.Mount{Source: "/home/me/.gitconfig", Target: "/home/agent/.gitconfig", Mode: container.ReadOnly}})
	c.Project("/work/empty")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config file: %v, %v", fi, err)
	}

	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Projects["/work/empty"]; ok {
		t.Error("empty project was saved")
	}
	p = c.Project("/work/proj")
	if e := p.Find("/work/lib"); e == nil || e.Mode != container.ReadOnly || !e.Disabled {
		t.Errorf("project mount = %+v", e)
	}
	if len(p.Allow) != 1 {
		t.Errorf("allow = %q", p.Allow)
	}
	if got := c.Global.Mounts; len(got) != 1 || got[0].Target != "/home/agent/.gitconfig" {
		t.Errorf("global mounts = %+v", got)
	}

	if !p.Remove("/work/lib") || p.Remove("/work/lib") {
		t.Error("Remove did not report correctly")
	}
	if !p.DisallowHost("registry.npmjs.org") || p.DisallowHost("registry.npmjs.org") {
		t.Error("DisallowHost did not report correctly")
	}
}
