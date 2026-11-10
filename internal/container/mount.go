package container

import (
	"encoding/csv"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Mode controls how the agent can access a mounted path.
type Mode string

const (
	ReadWrite Mode = "rw"
	ReadOnly  Mode = "ro"
	// Hidden masks a path inside another mount: directories are covered by an
	// empty tmpfs, files by /dev/null.
	Hidden Mode = "hidden"
)

// Mount makes a host file or directory visible to the agent, or hides it.
type Mount struct {
	Source string `json:"source"`           // absolute host path
	Target string `json:"target,omitempty"` // container path; defaults to Source
	Mode   Mode   `json:"mode"`

	dir bool // set by Resolve
}

// systemDirs are container paths that cannot be mounted over without
// breaking the container.
var systemDirs = []string{
	"/", "/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib64", "/proc", "/root",
	"/run", "/sbin", "/sys", "/usr", "/usr/bin", "/usr/lib", "/usr/local",
	"/usr/local/bin", "/usr/local/lib", "/usr/sbin", "/var",
}

func checkTarget(p string) error {
	if slices.Contains(systemDirs, p) {
		return fmt.Errorf("cannot mount over %s in the container", p)
	}
	return nil
}

// HostPath expands a leading ~ to the host home directory and resolves p
// relative to base.
func HostPath(p, base string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p), nil
}

// ContainerPath expands a leading ~ to the agent's home directory and checks
// that p is absolute.
func ContainerPath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = Home + p[1:]
	}
	if !path.IsAbs(p) {
		return "", fmt.Errorf("container path %q must be absolute", p)
	}
	p = path.Clean(p)
	return p, checkTarget(p)
}

// Contains reports whether p is dir or lies inside it.
func Contains(dir, p string) bool {
	_, ok := within(dir, p)
	return ok
}

func within(dir, p string) (string, bool) {
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// Resolve checks that all sources exist and fills in the container paths.
// A hidden path is placed relative to the innermost visible mount that
// contains it.
func Resolve(mounts []Mount) ([]Mount, error) {
	out := slices.Clone(mounts)
	for i := range out {
		m := &out[i]
		fi, err := os.Stat(m.Source)
		if err != nil {
			return nil, err
		}
		m.dir = fi.IsDir()
		switch m.Mode {
		case ReadWrite, ReadOnly:
			if m.Target == "" {
				m.Target = filepath.ToSlash(m.Source)
			}
		case Hidden:
		default:
			return nil, fmt.Errorf("%s: unknown mode %q", m.Source, m.Mode)
		}
	}

	for i := range out {
		if out[i].Mode != Hidden {
			continue
		}
		target, err := hiddenTarget(out[i].Source, out)
		if err != nil {
			return nil, err
		}
		out[i].Target = target
	}

	seen := map[string]string{}
	for _, m := range out {
		if err := checkTarget(m.Target); err != nil {
			return nil, fmt.Errorf("%s: %w", m.Source, err)
		}
		if other, ok := seen[m.Target]; ok {
			return nil, fmt.Errorf("%s and %s both map to %s in the container", other, m.Source, m.Target)
		}
		seen[m.Target] = m.Source
	}
	return out, nil
}

func hiddenTarget(source string, mounts []Mount) (string, error) {
	var parent *Mount
	var rel string
	for i, m := range mounts {
		if m.Mode == Hidden {
			continue
		}
		r, ok := within(m.Source, source)
		if !ok {
			continue
		}
		if r == "." {
			return "", fmt.Errorf("%s is mounted, so it cannot be hidden as well", source)
		}
		if parent == nil || len(m.Source) > len(parent.Source) {
			parent, rel = &mounts[i], r
		}
	}
	if parent == nil {
		return "", fmt.Errorf("%s is not inside a mounted directory, so there is nothing to hide", source)
	}
	return path.Join(parent.Target, filepath.ToSlash(rel)), nil
}

// opt returns the --mount value for a resolved mount.
func (m Mount) opt() string {
	switch {
	case m.Mode == Hidden && m.dir:
		return mountOpt("type=tmpfs", "target="+m.Target, "tmpfs-mode=0555")
	case m.Mode == Hidden:
		return mountOpt("type=bind", "source=/dev/null", "target="+m.Target, "readonly")
	case m.Mode == ReadOnly:
		return mountOpt("type=bind", "source="+m.Source, "target="+m.Target, "readonly")
	}
	return mountOpt("type=bind", "source="+m.Source, "target="+m.Target)
}

// mountOpt joins --mount fields. Docker parses the value as CSV, so fields
// containing commas or quotes must be quoted.
func mountOpt(fields ...string) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write(fields)
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}
