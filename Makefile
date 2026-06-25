SHELL := /bin/sh

BIN := rasterklang
CMD := ./cmd/rasterklang
DIST := dist
LICENSE_REPORT := $(DIST)/THIRD_PARTY_LICENSE_REPORT.md
LICENSE_REPORT_FLAGS ?= --fail-on-unknown
PACKAGE_MANIFEST_DIR := $(DIST)/package-manifests
RELEASE_BASE_URL ?= https://github.com/dnoegel/rasterklang-cli/releases/download
TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
VERSION ?=
BUILD_VERSION ?= $(if $(VERSION),$(VERSION),$(shell git describe --tags --dirty --always 2>/dev/null || echo dev))
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(BUILD_VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: help check test race staticcheck test-dist-targets test-package-manifests build clean license-report package-manifests identity-preflight release-preflight dist tag push-tag release check-version check-clean

help:
	@printf '%s\n' \
		'Targets:' \
		'  make check                        Run format, staticcheck, vet, and tests' \
		'  make test                         Run all tests' \
		'  make race                         Run all Go tests with the race detector' \
		'  make staticcheck                  Run staticcheck ./... when installed' \
		'  make test-dist-targets            Verify release archives for every target' \
		'  make test-package-manifests       Verify Homebrew/Scoop manifest generation' \
		'  make build                        Build local rasterklang binary' \
		'  make license-report               Generate dist/THIRD_PARTY_LICENSE_REPORT.md' \
		'  make package-manifests VERSION=v0.1.0 Generate Homebrew/Scoop package drafts' \
		'  make identity-preflight           Verify public release repo/module identity' \
		'  make dist VERSION=v0.1.0          Build release archives and package drafts in dist/' \
		'  make tag VERSION=v0.1.0           Create an annotated release tag' \
		'  make push-tag VERSION=v0.1.0      Push a release tag to origin' \
		'  make release VERSION=v0.1.0       Create and push a release tag'

test:
	go test ./...

race:
	go test -race ./...

check:
	@fmt="$$(gofmt -l .)"; \
	if [ -n "$$fmt" ]; then \
		echo "gofmt needed:"; \
		echo "$$fmt"; \
		exit 1; \
	fi
	node --check scripts/generate-license-report.mjs
	node --check scripts/generate-package-manifests.mjs
	node --check scripts/write-release-provenance.mjs
	node --check scripts/check-release-identity.mjs
	node --check scripts/test-package-manifests.mjs
	node --check scripts/test-race-contract.mjs
	node --check scripts/test-staticcheck-contract.mjs
	bash scripts/check-release-docs.sh
	$(MAKE) license-report
	node scripts/test-package-manifests.mjs
	node scripts/test-race-contract.mjs
	node scripts/test-staticcheck-contract.mjs
	$(MAKE) staticcheck
	go vet ./...
	go test ./...

staticcheck:
	@if ! command -v staticcheck >/dev/null 2>&1; then \
		echo "staticcheck not installed; skipping (CI installs it before make check)"; \
		exit 0; \
	fi; \
	out="$$(mktemp)"; \
	if staticcheck ./... >"$$out" 2>&1; then \
		rm -f "$$out"; \
		exit 0; \
	fi; \
	if grep -Eq 'module requires at least go1\.[0-9]+(\.[0-9]+)?, but Staticcheck was built with go1\.[0-9]+' "$$out"; then \
		cat "$$out"; \
		echo "staticcheck is installed but was built with an older Go toolchain; skipping local staticcheck (CI installs it before make check)"; \
		rm -f "$$out"; \
		exit 0; \
	fi; \
	cat "$$out"; \
	rm -f "$$out"; \
	exit 1

test-dist-targets:
	bash scripts/test-dist-targets.sh

test-package-manifests:
	node scripts/test-package-manifests.mjs

build:
	go build -ldflags="$(LDFLAGS)" -o $(BIN) $(CMD)

clean:
	rm -rf $(DIST) $(BIN)

license-report:
	mkdir -p "$(DIST)"
	node scripts/generate-license-report.mjs --project . --out "$(LICENSE_REPORT)" $(LICENSE_REPORT_FLAGS)

package-manifests: check-version
	node scripts/generate-package-manifests.mjs --version "$(VERSION)" --dist "$(DIST)" --out "$(PACKAGE_MANIFEST_DIR)" --release-base-url "$(RELEASE_BASE_URL)"

identity-preflight:
	node scripts/check-release-identity.mjs

release-preflight: identity-preflight

dist: clean license-report
	@set -eu; \
	mkdir -p "$(DIST)"; \
	for target in $(TARGETS); do \
		goos=$${target%/*}; \
		goarch=$${target#*/}; \
		label=$$goos; \
		if [ "$$label" = "darwin" ]; then label=macos; fi; \
		name="$(BIN)-$$label-$$goarch"; \
		output="$$name"; \
		if [ "$$goos" = "windows" ]; then output="$$name.exe"; fi; \
		stage_root="$(DIST)/.stage-$$name"; \
		package_dir="$$stage_root/$$name"; \
		echo "building $$name"; \
		GOOS=$$goos GOARCH=$$goarch CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o "$(DIST)/$$output" $(CMD); \
		chmod +x "$(DIST)/$$output"; \
		rm -rf "$$stage_root"; \
		mkdir -p "$$package_dir"; \
		cp "$(DIST)/$$output" "$$package_dir/"; \
		node scripts/write-release-provenance.mjs \
			--out "$$package_dir/RELEASE_PROVENANCE.json" \
			--name "$(BIN)" \
			--version "$(BUILD_VERSION)" \
			--commit "$(COMMIT)" \
			--date "$(DATE)" \
			--source-repository "https://github.com/dnoegel/rasterklang-cli" \
			--artifact-kind "cli-archive" \
			--artifact-name "$$name.tar.gz" \
			--target-os "$$goos" \
			--target-arch "$$goarch" \
			--build-command "make dist VERSION=$(VERSION)"; \
		cp README.md CHANGELOG.md CONTRIBUTING.md LICENSE SECURITY.md THIRD_PARTY_NOTICES.md "$(LICENSE_REPORT)" "$$package_dir/"; \
		tar -C "$$stage_root" -czf "$(DIST)/$$name.tar.gz" "$$name"; \
		rm -rf "$$stage_root" "$(DIST)/$$output"; \
		( \
			cd "$(DIST)"; \
			if command -v sha256sum >/dev/null 2>&1; then \
				sha256sum "$$name.tar.gz"; \
			else \
				shasum -a 256 "$$name.tar.gz"; \
			fi > "$$name.tar.gz.sha256" \
		); \
	done
	$(MAKE) package-manifests VERSION="$(VERSION)" RELEASE_BASE_URL="$(RELEASE_BASE_URL)"

tag: check-version check-clean
	@if git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null; then \
		echo "tag $(VERSION) already exists"; \
		exit 1; \
	fi
	git tag -a "$(VERSION)" -m "$(BIN) $(VERSION)"

push-tag: check-version
	git push origin "$(VERSION)"

release: release-preflight tag push-tag

check-version:
	@test -n "$(VERSION)" || { echo "VERSION is required, for example VERSION=v0.1.0"; exit 1; }
	@case "$(VERSION)" in \
		v[0-9]*.[0-9]*.[0-9]*) ;; \
		*) echo "VERSION must look like v0.1.0"; exit 1 ;; \
	esac

check-clean:
	@test -z "$$(git status --porcelain)" || { echo "working tree is dirty; commit changes before tagging"; exit 1; }
