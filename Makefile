.PHONY: build test fmt clean hooks release-check

VERSION ?= dev
TMPDIR ?= $(CURDIR)/tmp
GOCACHE ?= $(CURDIR)/.gocache
LDFLAGS := -s -w -X github.com/mugenkunou/ws-tool/cmd.appVersion=$(VERSION)

build:
	@mkdir -p "$(TMPDIR)" "$(GOCACHE)"
	TMPDIR="$(TMPDIR)" GOCACHE="$(GOCACHE)" go build -ldflags "$(LDFLAGS)" -o ws .

test:
	@mkdir -p "$(TMPDIR)" "$(GOCACHE)"
	TMPDIR="$(TMPDIR)" GOCACHE="$(GOCACHE)" go test ./...

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
	@mkdir -p "$(TMPDIR)" "$(GOCACHE)"
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
	TMPDIR="$(TMPDIR)" GOCACHE="$(GOCACHE)" go vet ./...
	TMPDIR="$(TMPDIR)" GOCACHE="$(GOCACHE)" go test -race -count=1 ./...
