package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"exileagent/internal/config"
	"exileagent/internal/container"
)

// setup creates a project with a .git directory and an .idea directory in a
// fake home directory and points the exile config into it.
func setup(t *testing.T) (home, dir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("EXILE_CONFIG", filepath.Join(home, ".config", "exile", "config.json"))
	dir = filepath.Join(home, "proj")
	for _, d := range []string{".git/hooks", ".idea", "src"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, dir
}

func mount(source string, mode container.Mode) config.Entry {
	return config.Entry{Mount: container.Mount{Source: source, Mode: mode}}
}

func TestMounts(t *testing.T) {
	_, dir := setup(t)
	shared := t.TempDir()
	cfg := &config.Config{}
	cfg.Global.Add(mount(shared, container.ReadWrite))
	cfg.Project(dir).Add(mount(shared, container.ReadOnly))
	cfg.Project(dir).Add(mount(filepath.Join(dir, ".idea"), container.ReadWrite))
	off := mount(filepath.Join(dir, "src"), container.Hidden)
	off.Disabled = true
	cfg.Project(dir).Add(off)

	got := map[string]Entry{}
	for _, e := range Mounts(cfg, dir) {
		got[e.Source] = e
	}
	for source, want := range map[string]Entry{
		dir:                         {container.Mount{Source: dir, Mode: container.ReadWrite}, ScopeDefault},
		shared:                      {container.Mount{Source: shared, Mode: container.ReadOnly}, ScopeProject},
		filepath.Join(dir, ".idea"): {container.Mount{Source: filepath.Join(dir, ".idea"), Mode: container.ReadWrite}, ScopeProject},
		filepath.Join(dir, ".git"):  {container.Mount{Source: filepath.Join(dir, ".git"), Target: filepath.Join(dir, ".git"), Mode: container.ReadOnly}, ScopeProtected},
	} {
		if got[source] != want {
			t.Errorf("%s: got %+v, want %+v", source, got[source], want)
		}
	}
	if _, ok := got[filepath.Join(dir, "src")]; ok {
		t.Error("disabled entry is mounted")
	}
	if len(got) != 4 {
		t.Errorf("got %d mounts: %+v", len(got), got)
	}

	unprotected := Unprotected(Mounts(cfg, dir))
	if !slices.Contains(unprotected, filepath.Join(dir, ".envrc")) || slices.Contains(unprotected, filepath.Join(dir, ".git")) {
		t.Errorf("Unprotected = %q", unprotected)
	}
}

func TestProtectedFollowsTarget(t *testing.T) {
	_, dir := setup(t)
	cfg := &config.Config{}
	lib := filepath.Join(dir, "src")
	if err := os.Mkdir(filepath.Join(lib, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Project(dir).Add(config.Entry{Mount: container.Mount{Source: lib, Target: "/mnt/lib", Mode: container.ReadWrite}})
	for _, e := range Mounts(cfg, dir) {
		if e.Source == filepath.Join(lib, ".git") && e.Target != "/mnt/lib/.git" {
			t.Errorf("protected .git of /mnt/lib mounted at %s", e.Target)
		}
	}
}

func TestResolveRejectsSensitive(t *testing.T) {
	home, dir := setup(t)
	for _, p := range []string{".ssh", ".config/exile"} {
		if err := os.MkdirAll(filepath.Join(home, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "exile", "agents.conf"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, mounts := range map[string][]container.Mount{
		"home":             {{Source: home, Mode: container.ReadOnly}},
		"inside sensitive": {{Source: filepath.Join(home, ".ssh"), Mode: container.ReadOnly}},
		"config dir":       {{Source: filepath.Join(home, ".config"), Mode: container.ReadOnly}},
		"agents.conf":      {{Source: filepath.Join(home, ".config", "exile", "agents.conf"), Mode: container.ReadOnly}},
	} {
		var entries []Entry
		for _, m := range mounts {
			entries = append(entries, Entry{m, ScopeProject})
		}
		if _, err := Resolve(entries); err == nil {
			t.Errorf("%s: Resolve succeeded", name)
		}
	}

	// Hiding the sensitive directories makes their parent mountable.
	entries := []Entry{
		{container.Mount{Source: home, Mode: container.ReadOnly}, ScopeProject},
		{container.Mount{Source: filepath.Join(home, ".ssh"), Mode: container.Hidden}, ScopeProject},
		{container.Mount{Source: filepath.Join(home, ".config", "exile"), Mode: container.Hidden}, ScopeProject},
	}
	if _, err := Resolve(entries); err != nil {
		t.Errorf("home with .ssh hidden: %v", err)
	}

	cfg := &config.Config{}
	if err := Validate(cfg, dir); err != nil {
		t.Errorf("plain project: %v", err)
	}
	cfg.Global.Add(mount(filepath.Join(dir, "src"), container.Hidden))
	if err := Validate(cfg, dir); err == nil {
		t.Error("global hidden entry accepted")
	}
}

func TestHosts(t *testing.T) {
	cfg := &config.Config{}
	cfg.Global.AllowHost("proxy.golang.org")
	cfg.Project("/p").AllowHost("registry.npmjs.org")
	cfg.Project("/p").AllowHost("api.anthropic.com")

	var names []string
	for _, h := range Hosts(cfg, "/p", "claude") {
		names = append(names, h.Name+"@"+h.Scope)
	}
	want := []string{
		"api.anthropic.com@claude", "console.anthropic.com@claude", "platform.claude.com@claude", "claude.ai@claude",
		"proxy.golang.org@global", "registry.npmjs.org@project",
	}
	if !slices.Equal(names, want) {
		t.Errorf("Hosts = %q, want %q", names, want)
	}
	if hosts := Hosts(cfg, "/other", "shell"); len(hosts) != 1 || hosts[0].Name != "proxy.golang.org" {
		t.Errorf("shell hosts = %+v", hosts)
	}
}
