SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
SERVER_DIR := $(ROOT_DIR)/server
WEB_DIR := $(SERVER_DIR)/web
ZASHBOARD_DIR := $(SERVER_DIR)/zashboard
DIST_DIR ?= $(ROOT_DIR)/dist
SERVER_BINARY ?= $(DIST_DIR)/poolnext-server

GO ?= go
PNPM ?= corepack pnpm
PYTHON ?= python3
VERSION ?= dev
COMMIT ?= $(shell git -C "$(ROOT_DIR)" rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
CGO_ENABLED ?= 0
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
GO_BUILD_FLAGS ?= -trimpath -buildvcs=false
GO_LDFLAGS ?= -s -w -X poolx/internal/pkg/common.Version=$(VERSION)
SWAG_VERSION ?= v1.16.4
ACTIONLINT_VERSION ?= v1.7.7
DOCKER_IMAGE ?= poolnext-ci:$(COMMIT)
EXPECTED_GO_VERSION ?= go1.24.13
EXPECTED_NODE_VERSION ?= v20.19.5
REQUIRE_CROSS_CHANNEL_BINARY_MATCH ?= $(shell if [[ "$$($(GO) version 2>/dev/null | awk '{print $$3}')" == "$(EXPECTED_GO_VERSION)" && "$$(node --version 2>/dev/null)" == "$(EXPECTED_NODE_VERSION)" ]]; then printf 1; else printf 0; fi)

WEB_DEPS_STAMP := $(WEB_DIR)/node_modules/.poolnext-lock-stamp
ZASHBOARD_DEPS_STAMP := $(ZASHBOARD_DIR)/node_modules/.poolnext-lock-stamp
PRODUCTION_SMOKE_SCRIPT := $(ZASHBOARD_DIR)/scripts/production-smoke.mjs

.PHONY: help deps run quality ci test test-go test-go-shuffle test-go-race \
	test-web test-zashboard test-baseline coverage-go \
	format-check format-check-go format-check-web format-check-zashboard \
	lint lint-go lint-web lint-zashboard typecheck typecheck-web typecheck-zashboard \
	build build-server build-server-from-assets build-assets build-web build-zashboard \
	verify-assets verify-embedded-assets verify-swagger verify-workflows \
	verify-reproducible playwright-install smoke-production container-smoke \
	clean clean-assets

help: ## Show the unified development commands.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-30s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: $(WEB_DEPS_STAMP) $(ZASHBOARD_DEPS_STAMP) ## Install pinned local frontend dependencies.

$(WEB_DEPS_STAMP): $(WEB_DIR)/package.json $(WEB_DIR)/pnpm-lock.yaml
	cd "$(WEB_DIR)" && HUSKY=0 CI=1 $(PNPM) install --frozen-lockfile
	@touch "$@"

$(ZASHBOARD_DEPS_STAMP): $(ZASHBOARD_DIR)/package.json $(ZASHBOARD_DIR)/pnpm-lock.yaml
	cd "$(ZASHBOARD_DIR)" && HUSKY=0 CI=1 $(PNPM) install --frozen-lockfile
	@touch "$@"

run: build-assets ## Build embedded assets and run the canonical server entrypoint.
	cd "$(SERVER_DIR)" && $(GO) run -tags embedassets $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)" .

quality: format-check lint typecheck test ## Run the read-only repository quality gate.

ci: ## Run the complete local CI gate, including reproducibility, browser, and container smoke tests.
	@$(MAKE) --no-print-directory quality
	@$(MAKE) --no-print-directory test-go-shuffle test-go-race coverage-go verify-swagger verify-workflows test-baseline
	@$(MAKE) --no-print-directory verify-reproducible
	@$(MAKE) --no-print-directory playwright-install smoke-production
	@$(MAKE) --no-print-directory container-smoke

test: test-go test-web test-zashboard ## Run backend and frontend tests.

test-go: ## Run ordinary Go tests without requiring generated frontend assets.
	cd "$(SERVER_DIR)" && $(GO) test -count=1 ./...

test-go-shuffle: ## Run Go tests in randomized order.
	cd "$(SERVER_DIR)" && $(GO) test -shuffle=on -count=1 ./...

test-go-race: ## Run the Go race detector with randomized test order.
	cd "$(SERVER_DIR)" && $(GO) test -race -shuffle=on -count=1 ./...

coverage-go: ## Produce Go atomic coverage output.
	cd "$(SERVER_DIR)" && $(GO) test -shuffle=on -count=1 -covermode=atomic -coverprofile=coverage.out ./...
	cd "$(SERVER_DIR)" && $(GO) tool cover -func=coverage.out | tee coverage.txt

test-web: $(WEB_DEPS_STAMP) ## Run WebUI unit and build-script tests.
	cd "$(WEB_DIR)" && $(PNPM) run test
	cd "$(WEB_DIR)" && $(PNPM) run test:build-scripts

test-zashboard: typecheck-zashboard ## Run the focused Zashboard security regression suite.
	cd "$(ZASHBOARD_DIR)" && $(PNPM) run test:notification-safety

test-baseline: ## Run the disposable Phase 0 baseline-tool self-test.
	cd "$(ROOT_DIR)" && PYTHON="$(PYTHON)" scripts/audit/selftest_phase0_baseline.sh

format-check: format-check-go format-check-web format-check-zashboard ## Check formatting without modifying files.

format-check-go: ## Check Go formatting while excluding dependencies and generated assets.
	@files="$$(find "$(SERVER_DIR)" \
		\( -path '*/node_modules' -o -path '*/web/build' -o -path '*/zashboard/dist' -o -path '*/vendor' \) -prune -o \
		-type f -name '*.go' -print0 | xargs -0 -r gofmt -l)"; \
	if [[ -n "$$files" ]]; then printf '%s\n' "$$files"; exit 1; fi

format-check-web: $(WEB_DEPS_STAMP) ## Check WebUI formatting.
	cd "$(WEB_DIR)" && $(PNPM) run format:check

format-check-zashboard: $(ZASHBOARD_DEPS_STAMP) ## Check Zashboard formatting.
	cd "$(ZASHBOARD_DIR)" && $(PNPM) run format:check

lint: lint-go lint-web lint-zashboard ## Run read-only linters.

lint-go: ## Run go vet.
	cd "$(SERVER_DIR)" && $(GO) vet ./...

lint-web: $(WEB_DEPS_STAMP) ## Lint WebUI sources with zero warnings.
	cd "$(WEB_DIR)" && $(PNPM) run lint

lint-zashboard: $(ZASHBOARD_DEPS_STAMP) ## Lint Zashboard sources with zero warnings.
	cd "$(ZASHBOARD_DIR)" && $(PNPM) run lint

typecheck: typecheck-web typecheck-zashboard ## Type-check both frontends.

typecheck-web: $(WEB_DEPS_STAMP) ## Type-check WebUI sources.
	cd "$(WEB_DIR)" && $(PNPM) run typecheck

typecheck-zashboard: $(ZASHBOARD_DEPS_STAMP) ## Type-check Zashboard sources.
	cd "$(ZASHBOARD_DIR)" && $(PNPM) run type-check

build: build-assets ## Rebuild both frontends and produce an embedded server binary.
	@$(MAKE) --no-print-directory build-server-from-assets

build-server: build ## Compatibility alias for the complete production build.

build-assets: build-web build-zashboard
	@$(MAKE) --no-print-directory verify-assets

build-web: $(WEB_DEPS_STAMP) ## Clean and build the embedded WebUI export.
	cd "$(WEB_DIR)" && NEXT_BUILD_ID="$(COMMIT)" NEXT_PUBLIC_APP_VERSION="$(VERSION)" $(PNPM) run build:embedded

build-zashboard: $(ZASHBOARD_DEPS_STAMP) ## Clean and build the embedded Zashboard export.
	cd "$(ZASHBOARD_DIR)" && ZASHBOARD_COMMIT_ID="$(COMMIT)" $(PNPM) run build:embedded

verify-assets: ## Verify both generated frontend trees contain entrypoints and executable chunks.
	@test -s "$(WEB_DIR)/build/index.html" || { echo "missing WebUI build/index.html" >&2; exit 1; }
	@test -n "$$(find "$(WEB_DIR)/build/_next/static" -type f -name '*.js' -print -quit 2>/dev/null)" || { echo "missing WebUI JavaScript chunks" >&2; exit 1; }
	@test -s "$(ZASHBOARD_DIR)/dist/index.html" || { echo "missing Zashboard dist/index.html" >&2; exit 1; }
	@test -n "$$(find "$(ZASHBOARD_DIR)/dist/assets" -type f -name '*.js' -print -quit 2>/dev/null)" || { echo "missing Zashboard JavaScript chunks" >&2; exit 1; }

verify-embedded-assets: verify-assets ## Compile-test that production embeds both frontends.
	cd "$(SERVER_DIR)" && $(GO) test -tags embedassets -run '^TestProductionBuildEmbedsBothFrontends$$' -count=1 .

build-server-from-assets: verify-embedded-assets ## Build the canonical server from existing verified assets.
	@mkdir -p "$(dir $(SERVER_BINARY))"
	cd "$(SERVER_DIR)" && CGO_ENABLED="$(CGO_ENABLED)" GOOS="$(GOOS)" GOARCH="$(GOARCH)" \
		$(GO) build -tags embedassets $(GO_BUILD_FLAGS) -ldflags "$(GO_LDFLAGS)" -o "$(SERVER_BINARY)" .

verify-swagger: ## Regenerate Swagger in a temporary directory and compare it with tracked output.
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cd "$(SERVER_DIR)"; \
	$(GO) run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION) init -g main.go -o "$$tmp/docs" >/dev/null; \
	diff -ru docs "$$tmp/docs"

verify-workflows: ## Validate GitHub Actions workflow syntax.
	cd "$(ROOT_DIR)" && $(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

verify-reproducible: deps ## Build twice from clean assets and compare assets and binaries byte-for-byte.
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	$(MAKE) --no-print-directory clean-assets; \
	$(MAKE) --no-print-directory build SERVER_BINARY="$$tmp/server-first"; \
	(cd "$(SERVER_DIR)" && find web/build zashboard/dist -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) > "$$tmp/assets-first"; \
	$(MAKE) --no-print-directory clean-assets; \
	$(MAKE) --no-print-directory build SERVER_BINARY="$$tmp/server-second"; \
	(cd "$(SERVER_DIR)" && find web/build zashboard/dist -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) > "$$tmp/assets-second"; \
	cmp "$$tmp/assets-first" "$$tmp/assets-second"; \
	cmp "$$tmp/server-first" "$$tmp/server-second"

playwright-install: $(WEB_DEPS_STAMP) ## Install the Chromium runtime used by production smoke tests.
	cd "$(WEB_DIR)" && $(PNPM) exec playwright install chromium

smoke-production: build ## Exercise the embedded binary on desktop and mobile Chromium profiles.
	node "$(PRODUCTION_SMOKE_SCRIPT)" --binary "$(SERVER_BINARY)"

container-smoke: build ## Verify hardened container runtime and cross-channel build consistency.
	@tmp="$$(mktemp -d)"; \
	container="poolnext-smoke-$${RANDOM}-$$PPID"; \
	extractor="$$container-extract"; \
	volume="$$container-data"; \
	image="$(DOCKER_IMAGE)"; \
	cleanup() { docker rm -f "$$container" >/dev/null 2>&1 || true; docker rm -f "$$extractor" >/dev/null 2>&1 || true; docker volume rm "$$volume" >/dev/null 2>&1 || true; rm -rf "$$tmp"; }; \
	trap cleanup EXIT; \
	docker build --build-arg "VERSION=$(VERSION)" --build-arg "COMMIT=$(COMMIT)" --target frontend-assets --output "type=local,dest=$$tmp/docker-assets" "$(SERVER_DIR)"; \
	(cd "$(SERVER_DIR)" && find web/build zashboard/dist -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) > "$$tmp/host-assets"; \
	(cd "$$tmp/docker-assets" && find web/build zashboard/dist -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) > "$$tmp/docker-assets.sum"; \
	if ! cmp "$$tmp/host-assets" "$$tmp/docker-assets.sum"; then \
		if [[ "$(REQUIRE_CROSS_CHANNEL_BINARY_MATCH)" == "1" ]]; then \
			echo "host and Docker frontend assets differ" >&2; exit 1; \
		fi; \
		echo "host and Docker frontend assets differ; comparison is advisory because the local Node/Go toolchain is not the pinned release toolchain" >&2; \
	fi; \
	docker build --build-arg "VERSION=$(VERSION)" --build-arg "COMMIT=$(COMMIT)" --tag "$$image" "$(SERVER_DIR)"; \
	test "$$(docker run --rm "$$image" --version)" = "$(VERSION)"; \
	test "$$(docker run --rm --entrypoint /bin/sh "$$image" -c 'id -u')" = "10001"; \
	docker create --name "$$extractor" "$$image" >/dev/null; \
	docker cp "$$extractor:/usr/local/bin/poolnext-server" "$$tmp/docker-server"; \
	docker rm "$$extractor" >/dev/null; \
	if ! cmp "$(SERVER_BINARY)" "$$tmp/docker-server"; then \
		if [[ "$(REQUIRE_CROSS_CHANNEL_BINARY_MATCH)" == "1" ]]; then \
			echo "host and Docker binaries differ" >&2; exit 1; \
		fi; \
		echo "host and Docker binaries differ; comparison is advisory because the local Node/Go toolchain is not the pinned release toolchain" >&2; \
	fi; \
	docker volume create "$$volume" >/dev/null; \
	secret="$$(openssl rand -hex 32)"; \
	docker run --detach --name "$$container" --publish 127.0.0.1::3000 \
		--read-only --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
		--cap-drop ALL --security-opt no-new-privileges:true \
		--env GIN_MODE=release --env LOG_LEVEL=warn --env "SESSION_SECRET=$$secret" \
		--env SQLITE_PATH=/data/poolnext.db --env UPLOAD_PATH=/data/upload \
		--volume "$$volume:/data" "$$image" >/dev/null; \
	address="$$(docker port "$$container" 3000/tcp | head -n1)"; \
	for attempt in $$(seq 1 60); do \
		health="$$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$$container")"; \
		if [[ "$$health" == healthy ]] && curl --fail --silent "http://$$address/api/status" >/dev/null; then break; fi; \
		if [[ "$$attempt" == 60 ]]; then docker logs "$$container" >&2; exit 1; fi; \
		sleep 2; \
	done; \
	test "$$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$$container")" = true; \
	test "$$(docker inspect --format '{{.Config.User}}' "$$container")" = "10001:10001"; \
	test "$$(docker inspect --format '{{index .HostConfig.Tmpfs "/tmp"}}' "$$container")" = "rw,nosuid,nodev,noexec,size=16m"; \
	test "$$(docker inspect --format '{{json .HostConfig.CapDrop}}' "$$container")" = '["ALL"]'; \
	test "$$(docker inspect --format '{{json .HostConfig.SecurityOpt}}' "$$container")" = '["no-new-privileges:true"]'; \
	test "$$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Type}}:{{.Name}}{{end}}{{end}}' "$$container")" = "volume:$$volume"; \
	curl --fail --silent "http://$$address/" | grep -qi '<!doctype html'

clean: clean-assets ## Remove generated assets and repository build outputs.
	rm -rf "$(DIST_DIR)"

clean-assets: ## Remove generated frontend assets while keeping dependencies.
	cd "$(WEB_DIR)" && $(PNPM) run clean
	cd "$(ZASHBOARD_DIR)" && $(PNPM) run clean
