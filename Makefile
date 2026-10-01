.PHONY: build test test-local test-docker test-image test-image-clean fmt clean hooks release-check install uninstall

VERSION  ?= dev
TMPDIR   ?= $(HOME)/tmp
GOCACHE  ?= $(HOME)/.gocache
GOTMPDIR ?= $(HOME)/gotmp
LDFLAGS  := -s -w -X github.com/mugenkunou/ws-tool/cmd.appVersion=$(VERSION)

# ── install paths ─────────────────────────────────────────────────────────────
# Default to ~/.local/bin (no sudo needed, on PATH for modern Linux/macOS).
# Override: make install PREFIX=/usr/local
PREFIX   ?= $(HOME)/.local
BINDIR   := $(PREFIX)/bin

# Docker image name used for test runs.
TEST_IMAGE := ws-tool-test:local
# Stamp file: present ↔ image is up-to-date relative to Dockerfile.test.
DOCKER_STAMP := .docker-image-stamp

# ── build ─────────────────────────────────────────────────────────────────────

build:
	@mkdir -p "$(TMPDIR)" "$(GOCACHE)" "$(GOTMPDIR)"
	TMPDIR="$(TMPDIR)" GOCACHE="$(GOCACHE)" GOTMPDIR="$(GOTMPDIR)" go build -ldflags "$(LDFLAGS)" -o ws .

# ── test ──────────────────────────────────────────────────────────────────────
# Default: run tests inside Docker for full isolation.
# Override with LOCAL=1 to run directly on the host machine (for CI and
# targeted debugging: make test LOCAL=1).

test:
ifdef LOCAL
	@$(MAKE) test-local
else
	@$(MAKE) test-docker
endif

# test-local: run tests directly on the host. Relies on a home-directory TMPDIR
# plus repo-local GOCACHE to work around noexec /tmp on this machine.
test-local:
	@mkdir -p "$(TMPDIR)" "$(GOCACHE)" "$(GOTMPDIR)"
	TMPDIR="$(TMPDIR)" GOCACHE="$(GOCACHE)" GOTMPDIR="$(GOTMPDIR)" go test ./...

# test-docker: build the image if needed, then run tests inside a container.
# The source tree is bind-mounted read-only; only cache/tmp dirs are writable
# so container-written files are owned cleanly by the host user.
test-docker: $(DOCKER_STAMP)
	@mkdir -p "$(TMPDIR)" "$(GOCACHE)" "$(GOTMPDIR)"
	docker run --rm --init \
		--user "$$(id -u):$$(id -g)" \
		-v "$(CURDIR):/workspace:ro" \
		-v "$(TMPDIR):/workspace/tmp:rw" \
		-v "$(CURDIR)/.gocache:/workspace/.gocache:rw" \
		-v "$(GOTMPDIR):/workspace/.gotmp:rw" \
		-w /workspace \
		-e HOME=/tmp \
		-e GOFLAGS=-mod=readonly \
		-e GONOSUMDB='*' \
		-e GONOSUMCHECK='*' \
		-e TMPDIR=/workspace/tmp \
		-e GOCACHE=/workspace/.gocache \
		-e GOTMPDIR=/workspace/.gotmp \
		-e XDG_CONFIG_HOME= \
		-e PASSWORD_STORE_DIR= \
		-e WS_CRONTAB_FILE= \
		-e GIT_CONFIG_GLOBAL= \
		-e DISPLAY= \
		$(TEST_IMAGE) \
		go test ./...

# test-image: build (or rebuild) the Docker test image and update the stamp.
# Depends on Dockerfile.test so Make rebuilds automatically when it changes.
$(DOCKER_STAMP): Dockerfile.test
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "ERROR: Docker not found. Run tests on the host with: make test LOCAL=1"; \
		exit 1; \
	fi
	docker build --pull=false -f Dockerfile.test -t $(TEST_IMAGE) .
	@touch $(DOCKER_STAMP)

test-image: $(DOCKER_STAMP)

# test-image-clean: remove the stamp and the Docker image, forcing a full
# rebuild on the next test-docker / test-image invocation.
test-image-clean:
	rm -f $(DOCKER_STAMP)
	docker rmi $(TEST_IMAGE) 2>/dev/null || true

# ── install / uninstall ───────────────────────────────────────────────────────
# Installs the locally compiled binary to $(BINDIR)/ws.
# The binary is tagged with a marker comment in the file itself (via a wrapper
# approach is not possible for a binary), so we write a tiny sidecar file
# $(BINDIR)/.ws-source=make next to it.  That lets uninstall know it owns the
# binary and avoids clobbering a release binary the user dropped in manually.
#
# If a binary already exists without the sidecar (i.e. installed from a GitHub
# release), install aborts with a clear message so the two paths never collide.

install: build
	@mkdir -p "$(BINDIR)"
	@if [ -f "$(BINDIR)/ws" ] && [ ! -f "$(BINDIR)/.ws-source" ]; then \
		echo "ERROR: $(BINDIR)/ws exists but was not installed by 'make install'."; \
		echo "       It was likely placed there from a GitHub release."; \
		echo "       Remove it first, or set a different PREFIX:"; \
		echo "         rm $(BINDIR)/ws && make install"; \
		echo "         make install PREFIX=~/.local"; \
		exit 1; \
	fi
	install -m 755 ws "$(BINDIR)/ws"
	@echo "make" > "$(BINDIR)/.ws-source"
	@echo "installed: $(BINDIR)/ws ($(VERSION))"

uninstall:
	@if [ ! -f "$(BINDIR)/.ws-source" ]; then \
		echo "ERROR: $(BINDIR)/ws was not installed by 'make install' — not removing."; \
		exit 1; \
	fi
	rm -f "$(BINDIR)/ws" "$(BINDIR)/.ws-source"
	@echo "removed: $(BINDIR)/ws"

# ── other targets ─────────────────────────────────────────────────────────────

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './.git/*')

clean:
	rm -f ws

hooks:
	@mkdir -p .git/hooks
	cp scripts/pre-push .git/hooks/pre-push
	chmod +x .git/hooks/pre-push
	@echo "pre-push hook installed"

release-check:
	@mkdir -p "$(TMPDIR)" "$(GOCACHE)" "$(GOTMPDIR)"
	@GITLEAKS_BIN="$$(command -v gitleaks || true)"; \
	if [ -z "$$GITLEAKS_BIN" ]; then \
		gobin="$$(go env GOBIN)"; \
		if [ -n "$$gobin" ] && [ -x "$$gobin/gitleaks" ]; then \
			GITLEAKS_BIN="$$gobin/gitleaks"; \
		fi; \
	fi; \
	if [ -z "$$GITLEAKS_BIN" ]; then \
		gopath="$$(go env GOPATH)"; \
		if [ -n "$$gopath" ] && [ -x "$$gopath/bin/gitleaks" ]; then \
			GITLEAKS_BIN="$$gopath/bin/gitleaks"; \
		fi; \
	fi; \
	if [ -z "$$GITLEAKS_BIN" ]; then \
		echo "gitleaks not found — install: go install github.com/zricethezav/gitleaks/v8@v8.21.2"; \
		exit 1; \
	fi; \
	"$$GITLEAKS_BIN" detect --source . --verbose
	TMPDIR="$(TMPDIR)" GOCACHE="$(GOCACHE)" GOTMPDIR="$(GOTMPDIR)" go vet ./...
	TMPDIR="$(TMPDIR)" GOCACHE="$(GOCACHE)" GOTMPDIR="$(GOTMPDIR)" go test -race -count=1 ./...
