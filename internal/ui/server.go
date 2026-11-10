// Package ui serves a local web interface for managing what agents can
// access.
package ui

import (
	"cmp"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"exileagent/internal/config"
	"exileagent/internal/container"
	"exileagent/internal/project"
)

//go:embed index.html
var index []byte

// Server serves the web interface. Every API request must carry Token, which
// only the link printed by "exile ui" contains, so other websites open in
// the browser cannot change the config.
type Server struct {
	Token string
	// Project is the directory "exile ui" was started in.
	Project string
	// Addr is the host:port the server listens on. Requests for other hosts
	// are rejected to defeat DNS rebinding.
	Addr string
	// Session is the agent session running in Project, if the server was
	// started by "exile start".
	Session Session

	mu sync.Mutex // serializes config changes
}

// Session is an agent session that can pick up changed settings while it
// runs.
type Session interface {
	Name() string
	// Pending reports whether the settings differ from those the agent runs
	// with.
	Pending() (bool, error)
	// Apply makes the agent use the current settings and describes what it
	// did.
	Apply() (string, error)
}

// Handler returns the HTTP handler of the interface.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
	mux.HandleFunc("GET /api/state", s.api(s.state))
	mux.HandleFunc("GET /api/browse", s.api(s.browse))
	mux.HandleFunc("POST /api/mount", s.api(s.addMount))
	mux.HandleFunc("PATCH /api/mount", s.api(s.updateMount))
	mux.HandleFunc("DELETE /api/mount", s.api(s.removeMount))
	mux.HandleFunc("POST /api/host", s.api(s.allowHost))
	mux.HandleFunc("DELETE /api/host", s.api(s.disallowHost))
	mux.HandleFunc("POST /api/apply", s.api(s.apply))
	mux.HandleFunc("POST /api/locate", s.api(s.locate))

	_, port, _ := net.SplitHostPort(s.Addr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.Addr && r.Host != "localhost:"+port {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) api(h func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Exile-Token")), []byte(s.Token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid token; open the link printed by exile ui"})
			return
		}
		s.mu.Lock()
		v, err := h(r)
		s.mu.Unlock()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			v = map[string]string{"error": err.Error()}
		}
		json.NewEncoder(w).Encode(v)
	}
}

type entryView struct {
	Source   string         `json:"source"`
	Target   string         `json:"target,omitempty"`
	Mode     container.Mode `json:"mode"`
	Disabled bool           `json:"disabled,omitempty"`
	Kind     string         `json:"kind"` // "dir", "file" or "missing"
	Scope    string         `json:"scope,omitempty"`
}

type hostView struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type stateView struct {
	Current  string       `json:"current"`
	Session  *sessionView `json:"session,omitempty"`
	Projects []string     `json:"projects"`
	Config   string       `json:"config"`
	Home     string       `json:"home"`
	// Project is the selected project; empty for the global scope.
	Project string      `json:"project"`
	Entries []entryView `json:"entries"`
	Allow   []string    `json:"allow"`
	// Automatic are the mounts exile adds on its own, and the global ones.
	Automatic  []entryView `json:"automatic"`
	AgentHosts []hostView  `json:"agentHosts"`
	HomeVolume string      `json:"homeVolume,omitempty"`
	Error      string      `json:"error,omitempty"`
	// AgentConfig is the path of the agent configuration. Of its variables,
	// only the names are exposed.
	AgentConfig string    `json:"agentConfig"`
	AgentVars   []varView `json:"agentVars"`
}

type sessionView struct {
	Name    string `json:"name"`
	Pending bool   `json:"pending"`
}

type varView struct {
	Name    string `json:"name"`
	Section string `json:"section"`
}

func kind(p string) string {
	fi, err := os.Stat(p)
	switch {
	case err != nil:
		return "missing"
	case fi.IsDir():
		return "dir"
	}
	return "file"
}

func (s *Server) state(r *http.Request) (any, error) {
	dir := s.Project
	if q := r.URL.Query(); q.Has("project") {
		dir = q.Get("project")
	}
	return s.stateFor(dir)
}

func (s *Server) stateFor(dir string) (*stateView, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	cfgPath, err := config.Path()
	if err != nil {
		return nil, err
	}
	projects := slices.Collect(maps.Keys(cfg.Projects))
	if !slices.Contains(projects, s.Project) {
		projects = append(projects, s.Project)
	}
	slices.Sort(projects)

	home, _ := os.UserHomeDir()
	v := &stateView{Current: s.Project, Projects: projects, Config: cfgPath, Home: home, Project: dir}
	if s.Session != nil {
		v.Session = &sessionView{Name: s.Session.Name()}
		v.Session.Pending, _ = s.Session.Pending()
	}
	scope := &cfg.Global
	if dir != "" {
		scope = cfg.Project(dir)
	}
	for _, e := range scope.Mounts {
		v.Entries = append(v.Entries, entryView{Source: e.Source, Target: e.Target, Mode: e.Mode, Disabled: e.Disabled, Kind: kind(e.Source)})
	}
	v.Allow = slices.Clone(scope.Allow)
	for _, name := range container.AgentNames() {
		for _, h := range container.Agents[name].Hosts {
			v.AgentHosts = append(v.AgentHosts, hostView{h, name})
		}
	}
	if v.AgentConfig, err = config.AgentsPath(); err != nil {
		return nil, err
	}
	agents, err := config.LoadAgents()
	if err != nil {
		v.Error = err.Error()
	}
	for _, section := range append([]string{config.SectionAll}, container.AgentNames()...) {
		for _, av := range agents[section] {
			v.AgentVars = append(v.AgentVars, varView{av.Name, section})
		}
	}

	if dir != "" {
		v.HomeVolume = container.HomeVolumeName(dir)
		entries := project.Mounts(cfg, dir)
		if resolved, err := project.Resolve(entries); err != nil {
			v.Error = err.Error()
		} else {
			entries = resolved
		}
		for _, e := range entries {
			if e.Scope != project.ScopeProject {
				v.Automatic = append(v.Automatic, entryView{Source: e.Source, Target: e.Target, Mode: e.Mode, Kind: kind(e.Source), Scope: e.Scope})
			}
		}
	}
	return v, nil
}

func (s *Server) apply(r *http.Request) (any, error) {
	if s.Session == nil {
		return nil, errors.New("no agent is running; the settings apply the next time you run exile start")
	}
	msg, err := s.Session.Apply()
	if err != nil {
		return nil, err
	}
	return map[string]string{"message": msg}, nil
}

type browseEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Dir       bool   `json:"dir"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

type browseView struct {
	Path    string        `json:"path"`
	Entries []browseEntry `json:"entries"`
}

func (s *Server) browse(r *http.Request) (any, error) {
	p := cmp.Or(r.URL.Query().Get("path"), s.Project)
	if !filepath.IsAbs(p) {
		return nil, fmt.Errorf("%q is not an absolute path", p)
	}
	p = filepath.Clean(p)
	dirEntries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	v := browseView{Path: p, Entries: []browseEntry{}}
	for _, e := range dirEntries {
		full := filepath.Join(p, e.Name())
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			fi, err := os.Stat(full)
			isDir = err == nil && fi.IsDir()
		}
		v.Entries = append(v.Entries, browseEntry{Name: e.Name(), Path: full, Dir: isDir, Sensitive: project.Sensitive(full)})
	}
	slices.SortFunc(v.Entries, func(a, b browseEntry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return v, nil
}

type mountRequest struct {
	Project  string         `json:"project"` // empty for the global scope
	Source   string         `json:"source"`
	Target   string         `json:"target"`
	Mode     container.Mode `json:"mode"`
	Disabled *bool          `json:"disabled"`
}

type hostRequest struct {
	Project string `json:"project"`
	Host    string `json:"host"`
}

func decode[T any](r *http.Request) (T, error) {
	var v T
	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&v)
	return v, err
}

// change applies fn to the scope of dir ("" for global) and saves the config
// if every affected project can still start, or could not start before
// either. Global changes are checked against the current project.
func (s *Server) change(dir string, fn func(*config.Scope) error) (any, error) {
	if dir != "" && !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%q is not an absolute path", dir)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	check := cmp.Or(dir, s.Project)
	before := project.Validate(cfg, check)
	scope := &cfg.Global
	if dir != "" {
		scope = cfg.Project(dir)
	}
	if err := fn(scope); err != nil {
		return nil, err
	}
	if err := project.Validate(cfg, check); err != nil && before == nil {
		return nil, err
	}
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	return s.stateFor(dir)
}

func (s *Server) addMount(r *http.Request) (any, error) {
	req, err := decode[mountRequest](r)
	if err != nil {
		return nil, err
	}
	return s.change(req.Project, func(scope *config.Scope) error {
		if !filepath.IsAbs(req.Source) {
			return fmt.Errorf("%q is not an absolute path", req.Source)
		}
		src := filepath.Clean(req.Source)
		if _, err := os.Stat(src); err != nil {
			return err
		}
		m := container.Mount{Source: src, Mode: req.Mode}
		if req.Target != "" && req.Mode != container.Hidden {
			if m.Target, err = container.ContainerPath(req.Target); err != nil {
				return err
			}
			if m.Target == filepath.ToSlash(src) {
				m.Target = ""
			}
		}
		scope.Add(config.Entry{Mount: m})
		return nil
	})
}

func (s *Server) updateMount(r *http.Request) (any, error) {
	req, err := decode[mountRequest](r)
	if err != nil {
		return nil, err
	}
	return s.change(req.Project, func(scope *config.Scope) error {
		e := scope.Find(req.Source)
		if e == nil {
			return fmt.Errorf("%s is not configured", req.Source)
		}
		if req.Mode != "" {
			e.Mode = req.Mode
		}
		if req.Disabled != nil {
			e.Disabled = *req.Disabled
		}
		return nil
	})
}

func (s *Server) removeMount(r *http.Request) (any, error) {
	req, err := decode[mountRequest](r)
	if err != nil {
		return nil, err
	}
	return s.change(req.Project, func(scope *config.Scope) error {
		if !scope.Remove(req.Source) {
			return fmt.Errorf("%s is not configured", req.Source)
		}
		return nil
	})
}

func (s *Server) allowHost(r *http.Request) (any, error) {
	req, err := decode[hostRequest](r)
	if err != nil {
		return nil, err
	}
	return s.change(req.Project, func(scope *config.Scope) error {
		host, err := container.NormalizeHost(req.Host)
		if err != nil {
			return err
		}
		if !scope.AllowHost(host) {
			return fmt.Errorf("%s is already allowed", host)
		}
		return nil
	})
}

func (s *Server) disallowHost(r *http.Request) (any, error) {
	req, err := decode[hostRequest](r)
	if err != nil {
		return nil, err
	}
	return s.change(req.Project, func(scope *config.Scope) error {
		if !scope.DisallowHost(req.Host) {
			return errors.New(req.Host + " is not allowed")
		}
		return nil
	})
}
