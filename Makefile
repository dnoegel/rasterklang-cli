SHELL := /bin/sh

BIN := zmk-sid
CMD := ./cmd/zmk-sid
DIST := dist
TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
VERSION ?=

.PHONY: help test build clean dist tag push-tag release check-version check-clean

help:
	@printf '%s\n' \
		'Targets:' \
		'  make test                         Run all tests' \
		'  make build                        Build local zmk-sid binary' \
		'  make dist                         Build release archives in dist/' \
		'  make tag VERSION=v0.1.0           Create an annotated release tag' \
		'  make push-tag VERSION=v0.1.0      Push a release tag to origin' \
		'  make release VERSION=v0.1.0       Create and push a release tag'

test:
	go test ./...

build:
	go build -o $(BIN) $(CMD)

clean:
	rm -rf $(DIST) $(BIN)

dist: clean
	@set -eu; \
	mkdir -p "$(DIST)"; \
	for target in $(TARGETS); do \
		goos=$${target%/*}; \
		goarch=$${target#*/}; \
		label=$$goos; \
		if [ "$$label" = "darwin" ]; then label=macos; fi; \
		name="$(BIN)-$$label-$$goarch"; \
		echo "building $$name"; \
		GOOS=$$goos GOARCH=$$goarch CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$(DIST)/$$name" $(CMD); \
		chmod +x "$(DIST)/$$name"; \
		tar -C "$(DIST)" -czf "$(DIST)/$$name.tar.gz" "$$name"; \
		rm "$(DIST)/$$name"; \
		( \
			cd "$(DIST)"; \
			if command -v sha256sum >/dev/null 2>&1; then \
				sha256sum "$$name.tar.gz"; \
			else \
				shasum -a 256 "$$name.tar.gz"; \
			fi > "$$name.tar.gz.sha256" \
		); \
	done

tag: check-version check-clean
	@if git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null; then \
		echo "tag $(VERSION) already exists"; \
		exit 1; \
	fi
	git tag -a "$(VERSION)" -m "$(BIN) $(VERSION)"

push-tag: check-version
	git push origin "$(VERSION)"

release: tag push-tag

check-version:
	@test -n "$(VERSION)" || { echo "VERSION is required, for example VERSION=v0.1.0"; exit 1; }
	@case "$(VERSION)" in \
		v[0-9]*.[0-9]*.[0-9]*) ;; \
		*) echo "VERSION must look like v0.1.0"; exit 1 ;; \
	esac

check-clean:
	@test -z "$$(git status --porcelain)" || { echo "working tree is dirty; commit changes before tagging"; exit 1; }
