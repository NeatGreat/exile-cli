# EXILE — exile your coding agent to a container

Coding agents like Claude Code are basically hyper-eager interns with full access to your computer: great for fixing bugs, but scary if a confused AI decides to wipe your home directory or snoop through your private SSH keys. Exile acts as a digital airlock by locking the agent inside an isolated Docker container. It can only see the specific project folder you explicitly give it, talk to a short allowlist of approved websites, and run terminal commands in isolation. If the AI goes rogue or executes a destructive script, the blast radius is trapped entirely inside the container, keeping your actual machine completely untouched.

```console
$ cd ~/code/my-project
$ exile start

  ███████╗██╗  ██╗██╗██╗     ███████╗
  ██╔════╝╚██╗██╔╝██║██║     ██╔════╝
  █████╗   ╚███╔╝ ██║██║     █████╗
  ██╔══╝   ██╔██╗ ██║██║     ██╔══╝
  ███████╗██╔╝ ██╗██║███████╗███████╗
  ╚══════╝╚═╝  ╚═╝╚═╝╚══════╝╚══════╝
  your coding agent, exiled to a container

  agent    claude
  project  ~/code/my-project  read & write
  access   1 more path · 1 hidden · 2 protected read-only
  network  HTTPS to 5 allowed hosts
  limits   3.00 CPUs · 6.0 GB memory · 1024 processes
  config   CLAUDE_CODE_OAUTH_TOKEN
```

- **One command:** `exile start` runs the agent in the current folder, with
  your terminal attached as usual.
- **Exact control:** `exile add`, `exile remove` and `exile allow` decide which
  files, folders and hosts the agent gets. You can also manage them in a web
  interface (`exile ui`).
- **Isolated by default:** no home directory, no credentials, no access to
  your local network, no root. The full list is under
  [How exile isolates the agent](#how-exile-isolates-the-agent).

## Supported agents

| Agent | Start | Credentials in `exile config` | Hosts always allowed |
| --- | --- | --- | --- |
| [Claude Code](https://github.com/anthropics/claude-code) | `exile start` | `CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY` | `api.anthropic.com`, `console.anthropic.com`, `platform.claude.com`, `claude.ai` |
| [Codex](https://github.com/openai/codex) | `exile start codex` | `OPENAI_API_KEY` | `api.openai.com`, `auth.openai.com`, `chatgpt.com` |
| [Cline](https://github.com/cline/cline) | `exile start cline` | `CLINE_API_KEY`, `CLINE_MODEL`, `CLINE_PROVIDER` | `api.cline.bot`, `api.workos.com`, `models.dev` |
| Shell (bash) | `exile start shell` | | |

All three agents come with the agent image. Each of them:

- **signs in** with the credentials from `exile config`, or inside the
  container (see [Add your credentials](#3-add-your-credentials)),
- **continues** its last session in the project (see [Sessions](#sessions)),
- **knows what it can access:** exile lists the granted paths in Claude
  Code's managed `CLAUDE.md`, Codex's global `AGENTS.md` and Cline's global
  rules.

The shell is for looking around as the agent, with the same access.

Cline talks to Cline's own API by default. With your own key for another
provider, such as Anthropic or OpenRouter, allow that provider's API host
too, for example `exile allow api.anthropic.com`.

## Setup

### 1. Requirements

- **Docker:** [Colima](https://github.com/abiosoft/colima), Docker Desktop or
  OrbStack on macOS, or Docker Engine on Linux.
- **Go 1.27** or newer, to build exile.

With Colima, give the VM enough room for an agent and your builds. Exile
limits each agent to 75% of the VM's memory and CPUs.

```sh
colima start --cpu 4 --memory 8
```

### 2. Install and set up

```sh
git clone <this repository> exile && cd exile
go build -o "$(go env GOPATH)/bin/exile" . && exile init
```

`exile init` does the rest in one go:

- checks that Docker is running,
- builds the agent image with Claude Code, Codex, Cline and the egress proxy,
- creates the agent configuration for your credentials,
- tells you if `exile` is not on your `PATH` yet, and how to add it.

Running it again is safe; it only does what is missing or outdated.
`exile start` also builds the image whenever it is missing or outdated. To
update the agents later, run `exile build --no-cache`.

### 3. Add your credentials

```sh
exile config
```

This opens the agent configuration in your editor. Each agent has its own
section, and only that agent gets its variables:

```ini
[all]
GIT_AUTHOR_NAME=Jane Doe

[claude]
# Subscription: run `claude setup-token` on your machine and paste the token.
CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-...
# Or an API key:
# ANTHROPIC_API_KEY=sk-ant-api03-...

[codex]
OPENAI_API_KEY=sk-...

[cline]
# A provider's API key and model; the provider is cline unless set.
CLINE_API_KEY=sk-ant-api03-...
CLINE_MODEL=claude-sonnet-5-5
CLINE_PROVIDER=anthropic
```

At start, exile copies the agent's variables into its container. With a
token or key configured, Claude Code skips its login and first-run setup,
Codex is logged in with the API key automatically, and Cline is set up
with the provider, key and model.

Without credentials, you can also log in inside the container: `/login` in
Claude Code, `codex login --device-auth` in `exile start shell`, or
**Sign in with Cline** in Cline. Select the login link with the mouse, or
press `c` in Claude Code, and it is on your Mac's clipboard. Each project
has its own agent home, so such a login only applies to one project.

### 4. Recommended hardening of the Docker VM

Containers share the kernel of the Docker VM. By default, Colima mounts your
whole home directory into the VM, read-write. If an agent ever escaped the
container, that is what it would find. Restrict the VM mounts to the folders
you work in. In `~/.colima/default/colima.yaml`:

```yaml
mounts:
  - location: ~/code
    writable: true
```

Then restart Colima with `colima stop && colima start`.

## Usage

```sh
exile init                       # set up, or update after pulling a new version
exile config                     # edit credentials and agent environment
exile start                      # Claude Code in the current folder
exile start codex                # Codex
exile start cline                # Cline
exile start shell                # a bash shell, to look around as the agent
exile start --new                # a fresh session instead of the last one
exile start -- --model opus      # arguments after -- go to the agent
exile start -v                   # show the agent's access before starting
```

### Files and folders

```sh
exile add ../shared-lib --ro                 # another folder, read-only
exile add .env secrets --hide                # hide paths inside the project
exile add . --ro                             # make the project itself read-only
exile add .git                               # let the agent commit (see below)
exile add ~/.gitconfig --to '~/.gitconfig' --ro --global   # for all projects
exile remove ../shared-lib
exile list                                   # everything the agent can access
```

Paths show up inside the container at the same location as on your machine,
so relative paths between projects keep working. `--to` mounts somewhere
else; `~` in `--to` is the agent's home (quote it so your shell does not
expand it).

### Network

```sh
exile allow registry.npmjs.org               # HTTPS to one more host
exile allow proxy.golang.org sum.golang.org --global
exile allow '*.githubusercontent.com'        # all subdomains
exile disallow registry.npmjs.org
exile start --network none shell             # no network at all
```

The agent's own API hosts are always allowed. When a session ends, exile lists
the hosts the agent tried to reach but was refused, so you can decide whether
to allow them.

### Web interface

While an agent runs, click **open settings ↗** in the exile status bar
above it. The settings page opens in your browser. Without a running
agent, `exile ui` opens it.

On the page you switch mounts on and off and change their access. To add
files and folders, drag them from Finder onto the page or click
**browse your Mac**; they show up at the same path in the container. You
also manage allowed hosts there, per project or for all projects.

Changes apply to a running agent right away:

- **Network changes** apply without interrupting the agent.
- **New files and folders, and credentials** restart the agent, which then
  continues its conversation where it left off.

The agent knows what it can access: exile lists the granted paths in Claude
Code's managed `CLAUDE.md`, Codex's global `AGENTS.md` and Cline's global
rules. Ask it about a file you just added and it knows where to look. Claude Code also gets them
as additional working directories, so it reads them without asking first.

The page only listens on `127.0.0.1` and only accepts requests that carry a
secret. That secret never enters the container: a click on the status bar
asks exile on your machine to open the page.

### You always know you are in exile

- **Status bar:** a bar above the agent reads `EXILED · claude · my-project`
  and links to the settings. Scroll back through the agent's output with the
  mouse wheel. Text you select with the mouse goes to your clipboard, as
  does text the agent copies, such as Claude Code's login link when you
  press `c`. This also works in Terminal, which ignores the OSC 52 escape
  sequence tmux uses otherwise. For your terminal's own selection, hold
  ⌥ Option (iTerm2) or Fn (Terminal). `exile start --no-bar` runs the agent
  without the bar.
- **Header:** before the agent starts, a header shows what it can reach.
- **Terminal title:** the tab reads `EXILED · claude · my-project` while the
  session runs.
- **Status line:** Claude Code shows an `EXILED` status line below its input.
- **Prompt:** the shell prompt in `exile start shell` starts with `(exiled)`.

### Sessions

Quit the agent whenever you like: the next `exile start` in the same project
continues its last session (`claude --continue`, `codex resume --last`,
`cline --id` with Cline's last session in the folder).
Sessions live in the project's agent home, so they survive restarts of the
container and of Docker. `exile start --new` starts a fresh session; with
agent arguments after `--`, exile starts the agent exactly as given.

```sh
exile ps                         # running agents
exile stop                       # stop the agents of the current folder
exile stop --all
```

## How exile isolates the agent

### Files

- **Only what you grant:** the container sees the project folder and the
  paths you added with `exile add`. Nothing else from your machine is
  mounted: no home directory, no SSH keys, no shell history.
- **Read-only and hidden paths:** `--ro` mounts are read-only at the kernel
  level. Hidden directories are covered by an empty read-only directory and
  hidden files by an empty file. The agent cannot read their content.
- **Credentials are off-limits:** exile refuses to mount `~/.ssh`, `~/.aws`,
  `~/.kube`, `~/.docker`, `~/.gnupg`, `~/.config/gh`, `~/.npmrc`, `~/.netrc`,
  `~/Library`, the Docker socket, your host's own `~/.claude`, `~/.codex`
  and `~/.cline`, any other socket, and any folder containing one of these
  unless that path is hidden.
- **Files your machine executes are read-only:** inside writable folders,
  `.git`, `.envrc`, `.idea`, `.vscode`, `.claude`, `.mcp.json`, `.codex`,
  `.cline`, `.clinerules`, `.husky`, `.pre-commit-config.yaml` and
  `lefthook.yml` are mounted read-only. A compromised agent can therefore not plant a git hook,
  `core.fsmonitor` setting, direnv script or IDE run configuration that your
  machine runs later. If the agent creates one of these files during a
  session, exile warns you afterwards. `exile add .git` lifts the protection
  for `.git` when you want the agent to commit.
- **The agent cannot widen its own access:** the config and the agent
  configuration live outside every project (`~/Library/Application
  Support/exile/` on macOS, `~/.config/exile/` on Linux). Exile refuses to
  mount them writable.
- **The container itself stays intact:** mounts over system directories such
  as `/usr` or `/etc` are rejected.

### Network

- **No route out:** each session gets its own internal Docker network with no
  route to the internet, your machine or other containers. External DNS does
  not resolve either.
- **One way out, through an allowlist:** a proxy container is the only member
  of that network with outside access. It only tunnels HTTPS (port 443) to
  allowlisted hosts. Plain HTTP, other ports and every other host are
  refused.
- **No detours into your network:** even for allowed host names, the proxy
  refuses to connect to loopback, private, link-local and CGNAT addresses.
  This shuts out your machine, your LAN, VPNs, other containers and cloud
  metadata endpoints.
- **Visible attempts:** refused connections are reported when the session
  ends.

### Privileges

- **Not root:** the agent runs as an unprivileged user whose UID matches
  yours, so files it creates belong to you.
- **No way back to root:** all Linux capabilities are dropped and
  `no-new-privileges` is set. There is no `sudo`, and no Docker socket.
- **Minimal proxy:** the proxy runs as `nobody` on a read-only filesystem,
  without capabilities.

### Resources

- **Limits:** memory (without swap), CPUs and the number of processes are
  limited, by default to 75% of the Docker VM and 1024 processes. A runaway
  agent or fork bomb cannot freeze the VM. Override with `--memory`, `--cpus`
  and `--pids-limit`.

### Environment and state

- **No inherited environment:** no environment variables from your shell
  reach the agent, except those you pass with `-e`.
- **Credentials per agent:** each agent only gets its own section of the
  agent configuration. The variables travel into the container as a file
  only root can read, which the entrypoint loads and deletes before
  starting the agent. They never show up in `docker inspect` or in the
  process list on your machine.
- **Separate homes:** each project has its own home volume, so an agent cannot
  read the history, settings or logins of another project. Use
  `--temp-home` for a throwaway home.
- **Less chatter:** Claude Code's telemetry, error reporting and auto-updater
  are switched off, as are Cline's telemetry and auto-updater.

### What exile does not protect against

- **Kernel exploits:** containers share the kernel of the Docker VM. A kernel
  exploit could escape the container. Restrict the VM's mounts (see Setup) to
  limit the damage.
- **Code you run yourself:** anything the agent writes into writable folders
  is code you might run later. Review its changes before you build, test or
  commit them on your machine.
- **Allowed hosts:** the agent can send data to every allowed host,
  including its own API, which receives your code by design.
- **Your clipboard:** what you copy in the session goes to your clipboard,
  and the agent can put text there too. It cannot read the clipboard. Check
  what you paste into a terminal on your machine.
- **Visible placeholders:** hidden files appear as empty files and hidden
  folders as empty folders. The agent can see that something is hidden, but
  not what.

## Development

```sh
go test ./...
go build -o exile .
```

| Path | Contents |
| --- | --- |
| `cmd/` | CLI commands |
| `internal/container/` | docker commands, mounts, sessions, image build |
| `internal/container/image/` | Dockerfile, entrypoint and egress proxy |
| `internal/project/` | effective access of a project and the safety rules |
| `internal/config/` | the config file |
| `internal/ui/` | web interface |
