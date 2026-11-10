FROM golang:1.27-alpine AS proxy
COPY proxy/main.go /src/main.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags=-s -o /exile-proxy /src/main.go

FROM node:24-slim

ARG CLAUDE_CODE_VERSION=latest
ARG CODEX_VERSION=latest
ARG CLINE_VERSION=latest

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl git jq less procps python3 ripgrep tini tmux unzip \
 && rm -rf /var/lib/apt/lists/*

RUN npm install -g \
      @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} \
      @openai/codex@${CODEX_VERSION} \
      cline@${CLINE_VERSION} \
 && npm cache clean --force

# Replace the image's "node" user with a neutral "agent" user. The entrypoint
# remaps its UID/GID to the host user's at startup.
RUN userdel --remove node \
 && groupadd --gid 1000 agent \
 && useradd --uid 1000 --gid agent --create-home --shell /bin/bash agent

# Mark the environment as exiled: the shell prompt reads "(exiled)". Claude
# Code's managed settings, with the EXILED status line, come with each
# session.
RUN echo exiled > /etc/debian_chroot \
 && install -d /etc/claude-code \
 && install -d -m 700 /run/exile \
 && install -d -m 755 /run/exile-requests \
 && install -m 622 /dev/null /run/exile-requests/requests
COPY statusline.sh /usr/local/bin/exile-statusline
COPY resume.sh /usr/local/bin/exile-resume
COPY session.sh /usr/local/bin/exile-session
COPY copy.sh /usr/local/bin/exile-copy
COPY tmux.conf /etc/exile/tmux.conf
COPY --from=proxy /exile-proxy /usr/local/bin/exile-proxy
COPY entrypoint.sh /usr/local/bin/exile-entrypoint

ENV HOME=/home/agent \
    LANG=C.UTF-8 \
    DISABLE_AUTOUPDATER=1 \
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 \
    CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1 \
    CLINE_NO_AUTO_UPDATE=1

# tini forwards stop signals to the whole process group (-g), so they reach
# the agent even when it runs inside tmux.
ENTRYPOINT ["/usr/bin/tini", "-g", "--", "/usr/local/bin/exile-entrypoint"]
CMD ["bash"]
