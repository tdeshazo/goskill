SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c

PYPROJECT := pyproject.toml
GO_MAIN := cmd/goskill/main.go
REMOTE ?= origin
BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD)
CURRENT_VERSION := $(shell awk -F '"' '/^version = / { print $$2; exit }' $(PYPROJECT))
VERSION ?=
TAG := v$(VERSION)

.PHONY: help version check-version set-version ensure-clean ensure-branch ensure-new-tag ensure-existing-tag release-commit release-tag push-release release require-version

help:
	@printf '%s\n' \
		'Targets:' \
		'  make version                         Print the current project version' \
		'  make check-version                   Ensure Go and Python versions match' \
		'  make set-version VERSION=0.2.1       Update Go and Python version declarations' \
		'  make release-commit VERSION=0.2.1    Commit synchronized version changes' \
		'  make release-tag VERSION=0.2.1       Create annotated tag v0.2.1' \
		'  make push-release VERSION=0.2.1      Push the current branch and tag' \
		'  make release VERSION=0.2.1           Commit, tag, and push a release'

version:
	@printf '%s\n' "$(CURRENT_VERSION)"

check-version:
	@py_version="$$(awk -F '"' '/^version = / { print $$2; exit }' "$(PYPROJECT)")"; \
	go_version="$$(awk -F '"' '/^var version = / { print $$2; exit }' "$(GO_MAIN)")"; \
	if [[ -z "$$py_version" || -z "$$go_version" ]]; then \
		echo "could not read version from $(PYPROJECT) or $(GO_MAIN)" >&2; \
		exit 1; \
	fi; \
	if [[ "$$py_version" != "$$go_version" ]]; then \
		echo "version mismatch: $(PYPROJECT)=$$py_version $(GO_MAIN)=$$go_version" >&2; \
		exit 1; \
	fi; \
	printf 'version %s\n' "$$py_version"

set-version: require-version
	@perl -0pi -e 's/^version = "[^"]+"/version = "$(VERSION)"/m' "$(PYPROJECT)"
	@perl -0pi -e 's/^var version = "[^"]+"/var version = "$(VERSION)"/m' "$(GO_MAIN)"
	@$(MAKE) check-version

ensure-clean:
	@if [[ -n "$$(git status --porcelain)" ]]; then \
		echo "working tree is not clean" >&2; \
		git status --short; \
		exit 1; \
	fi

ensure-branch:
	@if [[ "$(BRANCH)" == "HEAD" ]]; then \
		echo "cannot release from a detached HEAD" >&2; \
		exit 1; \
	fi

ensure-new-tag: require-version check-version ensure-branch
	@if git rev-parse "$(TAG)" >/dev/null 2>&1; then \
		echo "tag $(TAG) already exists" >&2; \
		exit 1; \
	fi

release-commit: require-version ensure-clean
	@$(MAKE) set-version VERSION="$(VERSION)"
	@git add "$(PYPROJECT)" "$(GO_MAIN)"
	@git commit -m "Release $(TAG)"

release-tag: ensure-new-tag
	@git tag -a "$(TAG)" -m "$(TAG)"

ensure-existing-tag: require-version check-version ensure-branch
	@if ! git rev-parse "$(TAG)" >/dev/null 2>&1; then \
		echo "tag $(TAG) does not exist; run make release-tag VERSION=$(VERSION) first" >&2; \
		exit 1; \
	fi

push-release: ensure-existing-tag
	@git push "$(REMOTE)" "$(BRANCH)" "$(TAG)"

release:
	@$(MAKE) release-commit VERSION="$(VERSION)" REMOTE="$(REMOTE)" BRANCH="$(BRANCH)"
	@$(MAKE) release-tag VERSION="$(VERSION)" REMOTE="$(REMOTE)" BRANCH="$(BRANCH)"
	@$(MAKE) push-release VERSION="$(VERSION)" REMOTE="$(REMOTE)" BRANCH="$(BRANCH)"

require-version:
	@if [[ -z "$(VERSION)" ]]; then \
		echo "VERSION is required, for example: make release VERSION=0.2.1" >&2; \
		exit 2; \
	fi
	@if [[ "$(VERSION)" == v* ]]; then \
		echo "VERSION must not include the leading v" >&2; \
		exit 2; \
	fi
	@if ! [[ "$(VERSION)" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$$ ]]; then \
		echo "VERSION must look like 0.2.1 or 0.2.1-rc.1" >&2; \
		exit 2; \
	fi
