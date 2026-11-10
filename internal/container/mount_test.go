package container

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// tree creates a project with a .env file, a secrets directory and a sibling
// library directory.
func tree(t *testing.T) (project, lib string) {
	t.Helper()
	root := t.TempDir()
	project, lib = filepath.Join(root, "project"), filepath.Join(root, "lib")
	for _, d := range []string{filepath.Join(project, "secrets"), lib} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return project, lib
}

func TestHostPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	for in, want := range map[string]string{
		"src":          "/base/src",
		"../lib":       "/lib",
		"/abs/./x":     "/abs/x",
		"~":            home,
		"~/.gitconfig": filepath.Join(home, ".gitconfig"),
	} {
		if got, err := HostPath(in, "/base"); err != nil || got != want {
			t.Errorf("HostPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestContainerPath(t *testing.T) {
	if got, _ := ContainerPath("~/.gitconfig"); got != Home+"/.gitconfig" {
		t.Errorf("got %q", got)
	}
	for _, bad := range []string{"relative", "/", "/x/..", "/lib", "/usr/local/bin/"} {
		if _, err := ContainerPath(bad); err == nil {
			t.Errorf("ContainerPath(%q) succeeded", bad)
		}
	}
}

func TestResolve(t *testing.T) {
	project, lib := tree(t)
	got, err := Resolve([]Mount{
		{Source: project, Mode: ReadWrite},
		{Source: lib, Target: "/mnt/lib", Mode: ReadOnly},
		{Source: filepath.Join(project, ".env"), Mode: Hidden},
		{Source: filepath.Join(project, "secrets"), Mode: Hidden},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{project, "/mnt/lib", project + "/.env", project + "/secrets"}
	var targets []string
	for _, m := range got {
		targets = append(targets, m.Target)
	}
	if !slices.Equal(targets, want) {
		t.Errorf("targets = %q, want %q", targets, want)
	}
	if got[2].dir || !got[3].dir {
		t.Errorf("dir flags = %v, %v", got[2].dir, got[3].dir)
	}
}

func TestResolveHiddenUsesInnermostMount(t *testing.T) {
	project, _ := tree(t)
	got, err := Resolve([]Mount{
		{Source: project, Mode: ReadWrite},
		{Source: filepath.Join(project, "secrets"), Target: "/s", Mode: ReadOnly},
		{Source: filepath.Join(project, "secrets"), Mode: Hidden},
	})
	if err == nil {
		t.Fatalf("hiding a mount root succeeded: %+v", got)
	}

	if err := os.WriteFile(filepath.Join(project, "secrets", "key"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = Resolve([]Mount{
		{Source: project, Mode: ReadWrite},
		{Source: filepath.Join(project, "secrets"), Target: "/s", Mode: ReadOnly},
		{Source: filepath.Join(project, "secrets", "key"), Mode: Hidden},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[2].Target != "/s/key" {
		t.Errorf("target = %q, want /s/key", got[2].Target)
	}
}

func TestResolveErrors(t *testing.T) {
	project, lib := tree(t)
	for name, mounts := range map[string][]Mount{
		"missing source":   {{Source: filepath.Join(project, "nope"), Mode: ReadOnly}},
		"unknown mode":     {{Source: project, Mode: "rwx"}},
		"hidden outside":   {{Source: project, Mode: ReadWrite}, {Source: lib, Mode: Hidden}},
		"duplicate target": {{Source: project, Target: "/x", Mode: ReadWrite}, {Source: lib, Target: "/x", Mode: ReadOnly}},
	} {
		if _, err := Resolve(mounts); err == nil {
			t.Errorf("%s: Resolve succeeded", name)
		}
	}
}

func TestMountOpt(t *testing.T) {
	project, _ := tree(t)
	mounts, err := Resolve([]Mount{
		{Source: project, Mode: ReadOnly},
		{Source: project + "/.env", Mode: Hidden},
		{Source: project + "/secrets", Mode: Hidden},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{
		"type=bind,source=" + project + ",target=" + project + ",readonly",
		"type=bind,source=/dev/null,target=" + project + "/.env,readonly",
		"type=tmpfs,target=" + project + "/secrets,tmpfs-mode=0555",
	} {
		if got := mounts[i].opt(); got != want {
			t.Errorf("opt() = %q, want %q", got, want)
		}
	}
	if got := mountOpt("type=bind", "source=/a,b"); got != `type=bind,"source=/a,b"` {
		t.Errorf("mountOpt with comma = %q", got)
	}
}
