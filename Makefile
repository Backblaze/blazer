# Local mirror of .github/workflows/ci.yml and changelog.yml. `make ci` runs the full gate.

GOLANGCI_LINT_VERSION := v2.14.0
CHANGIE_VERSION := v1.24.0
CHANGIE := go run github.com/miniscruff/changie@$(CHANGIE_VERSION)
BASE_REF ?= origin/main

.PHONY: ci build vet fmt fmt-check tidy-check verify lint test b2keys crosscompile changelog-check changelog-new

ci: build vet fmt-check tidy-check verify lint test b2keys crosscompile changelog-check

build:
	go build ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

tidy-check:
	go mod tidy -diff

verify:
	go mod verify

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --new-from-rev=$(BASE_REF) ./...

test:
	go test -race -shuffle=on ./...

b2keys:
	cd bin/b2keys && go build ./... && go test ./...

crosscompile:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=386 go build ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=arm go build ./...
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...

# When the branch changes non-test Go code or go.mod, it must add a .changes/unreleased entry
# (or a release's .changes/vX.Y.Z.md). Existing entries must parse.
changelog-check:
	@if git diff --name-only $(BASE_REF)...HEAD -- '*.go' '**/go.mod' ':!*_test.go' ':!.sdkharness/**' | grep -q .; then \
		git diff --name-only --diff-filter=A $(BASE_REF)...HEAD -- '.changes/unreleased/*.yaml' '.changes/v*.md' | grep -q . \
		|| { echo "No changelog entry: run 'make changelog-new' (or label the PR skip-changelog)."; exit 1; }; \
	fi
	@if ls .changes/unreleased/*.yaml > /dev/null 2>&1; then $(CHANGIE) batch auto --dry-run > /dev/null; fi

changelog-new:
	$(CHANGIE) new
