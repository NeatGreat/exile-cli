package container

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Result describes a finished session.
type Result struct {
	ExitCode int
	// Blocked lists the hosts the proxy refused, in order of first attempt.
	Blocked []string
}

// requestsFile is where the session inside the container writes requests to
// exile, one per line, such as "open-settings" when the status bar is
// clicked, or "copy BASE64" when text is copied. The agent can write to it
// too, so requests must be harmless.
const requestsFile = "/run/exile-requests/requests"

// maxRequest is the longest request line exile reads. exile-copy copies up
// to a million bytes, which take 1.33 million in base64.
const maxRequest = 2 << 20

// Run sets up the session, attaches the terminal to the agent and removes
// the containers and network once the agent exits. Requests from the
// session are passed to onRequest, if not nil.
func Run(s Spec, onRequest func(string)) (Result, error) {
	cmds, err := s.Commands()
	if err != nil {
		return Result{}, err
	}
	defer cleanup(s.Name)

	if err := runSteps(cmds[:len(cmds)-1]); err != nil {
		return Result{}, err
	}

	attach := exec.Command("docker", cmds[len(cmds)-1].Args...)
	attach.Stdin, attach.Stdout, attach.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Stay alive until the agent exits so that cleanup runs. Interrupts and
	// hangups reach docker directly through the terminal; termination
	// requests aimed at exile are passed on.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	if err := attach.Start(); err != nil {
		return Result{}, err
	}
	go func() {
		for sig := range sigs {
			if sig == syscall.SIGTERM {
				attach.Process.Signal(sig)
			}
		}
	}()
	attached := make(chan struct{})
	if onRequest != nil {
		go watchRequests(s.Name, attached, onRequest)
	}

	var res Result
	err = attach.Wait()
	close(attached)
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
	}
	if s.Network == NetworkFiltered {
		res.Blocked = blockedHosts(s.Name)
	}
	return res, nil
}

// watchRequests passes the requests written in the session's container to
// handle until the container stops or attached is closed.
func watchRequests(session string, attached <-chan struct{}, handle func(string)) {
	// The container starts with the attach command; wait until it runs.
	for {
		out, _ := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", session).Output()
		if strings.TrimSpace(string(out)) == "true" {
			break
		}
		select {
		case <-attached:
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	tail := exec.Command("docker", "exec", session, "tail", "-n0", "-F", requestsFile)
	out, err := tail.StdoutPipe()
	if err != nil || tail.Start() != nil {
		return
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(nil, maxRequest)
	for sc.Scan() {
		handle(strings.TrimSpace(sc.Text()))
	}
	tail.Wait()
}

func runSteps(steps []Step) error {
	for _, step := range steps {
		cmd := exec.Command("docker", step.Args...)
		if step.Input != nil {
			cmd.Stdin = bytes.NewReader(step.Input)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker %s: %s", step.Args[0], strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// ReloadProxy replaces the proxy of the running session s with one that
// allows s.Allow; the agent keeps running. It returns the hosts the old
// proxy refused.
func ReloadProxy(s Spec) ([]string, error) {
	blocked := blockedHosts(s.Name)
	if err := exec.Command("docker", "rm", "--force", proxyName(s.Name)).Run(); err != nil {
		return blocked, fmt.Errorf("docker rm: %w", err)
	}
	return blocked, runSteps(s.proxySteps())
}

// blockedHosts collects the hosts denied by the session's proxy.
func blockedHosts(session string) []string {
	out, _ := exec.Command("docker", "logs", proxyName(session)).CombinedOutput()
	var hosts []string
	seen := map[string]bool{}
	for line := range strings.Lines(string(out)) {
		if host, ok := strings.CutPrefix(strings.TrimSpace(line), "deny "); ok && !seen[host] {
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// cleanup removes the session's containers and network.
func cleanup(session string) {
	exec.Command("docker", "rm", "--force", session, proxyName(session)).Run()
	exec.Command("docker", "network", "rm", networkName(session)).Run()
}

// Container is a running agent container.
type Container struct {
	Name    string
	Agent   string
	Workdir string
	Status  string
}

// Running returns the running agent containers.
func Running() ([]Container, error) {
	out, err := dockerOutput("ps",
		"--filter", "label="+labelRole+"=agent",
		"--format", fmt.Sprintf(`{{.Names}}\t{{.Label %q}}\t{{.Label %q}}\t{{.Status}}`, labelAgent, labelWorkdir),
	)
	if err != nil {
		return nil, err
	}
	var containers []Container
	for line := range strings.Lines(string(out)) {
		f := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(f) != 4 {
			continue
		}
		containers = append(containers, Container{Name: f[0], Agent: f[1], Workdir: f[2], Status: f[3]})
	}
	return containers, nil
}

// Stop stops the named agent containers. The exile process that started
// each of them then removes the session.
func Stop(names ...string) error {
	_, err := dockerOutput(append([]string{"stop"}, names...)...)
	return err
}

// Prune removes the leftovers of sessions whose agent is no longer running,
// such as after the exile process was killed. A session that is starting
// right now counts as not running.
func Prune() error {
	running, err := Running()
	if err != nil {
		return err
	}
	alive := map[string]bool{}
	for _, c := range running {
		alive[c.Name] = true
	}
	format := fmt.Sprintf("{{.Label %q}}", labelSession)
	containers, err := dockerOutput("ps", "--all", "--filter", "label="+labelManaged+"=true", "--format", format)
	if err != nil {
		return err
	}
	networks, err := dockerOutput("network", "ls", "--filter", "label="+labelManaged+"=true", "--format", format)
	if err != nil {
		return err
	}
	for session := range strings.Lines(string(containers) + string(networks)) {
		if session = strings.TrimSpace(session); session != "" && !alive[session] {
			cleanup(session)
			alive[session] = true
		}
	}
	return nil
}

// Resources returns the number of CPUs and the memory in bytes available to
// Docker.
func Resources() (cpus int, memory int64, err error) {
	out, err := dockerOutput("info", "--format", "{{.NCPU}} {{.MemTotal}}")
	if err != nil {
		return 0, 0, err
	}
	_, err = fmt.Sscan(string(out), &cpus, &memory)
	return cpus, memory, err
}

func dockerOutput(args ...string) ([]byte, error) {
	out, err := exec.Command("docker", args...).Output()
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		return nil, fmt.Errorf("docker %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
	}
	return out, err
}
