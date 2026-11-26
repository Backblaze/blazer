# Local mirror of .github/workflows/ci.yml and changelog.yml. `make ci` runs the full gate.

GOLANGCI_LINT_VERSION := v2.14.0
CHANGIE_VERSION := v1.24.0
CHANGIE := go run github.com/miniscruff/changie@$(CHANGIE_VERSION)
BASE_REF ?= origin/main
TEST_FLAGS ?=

.PHONY: known-defects known-defects-32bit release-check ci build vet fmt fmt-check tidy-check verify lint test b2keys crosscompile crosscompile-one changelog-check changelog-new scripts-test api-compat docs-lint

ci: build vet fmt-check tidy-check verify lint test b2keys crosscompile changelog-check scripts-test docs-lint

build:
	go build ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)" || exit 1; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

tidy-check:
	go mod tidy -diff

verify:
	go mod verify

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --new-from-rev=$(BASE_REF) ./...

test:
	go test -race -shuffle=on $(TEST_FLAGS) ./...

b2keys:
	cd bin/b2keys && go build ./... && go test ./...

crosscompile:
	$(MAKE) crosscompile-one GOOS=linux GOARCH=amd64
	$(MAKE) crosscompile-one GOOS=linux GOARCH=386
	$(MAKE) crosscompile-one GOOS=linux GOARCH=arm
	$(MAKE) crosscompile-one GOOS=darwin GOARCH=arm64
	$(MAKE) crosscompile-one GOOS=windows GOARCH=amd64

crosscompile-one:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build ./...

# When the branch changes non-test Go code or go.mod, it must add a .changes/unreleased entry
# (or a release's .changes/vX.Y.Z.md). Existing entries must parse.
changelog-check:
	@git rev-parse --verify --quiet $(BASE_REF) > /dev/null || { echo "BASE_REF $(BASE_REF) not found; run 'git fetch origin' or pass BASE_REF=<ref>."; exit 1; }
	@if git diff --name-only $(BASE_REF)...HEAD -- '*.go' go.mod '**/go.mod' ':!*_test.go' ':!.sdkharness/**' | grep -q .; then \
		git diff --name-only --diff-filter=A $(BASE_REF)...HEAD -- '.changes/unreleased/*.yaml' '.changes/v*.md' | grep -q . \
		|| { echo "No changelog entry: run 'make changelog-new' (or label the PR skip-changelog)."; exit 1; }; \
	fi
	@if ls .changes/unreleased/*.yaml > /dev/null 2>&1; then $(CHANGIE) batch auto --dry-run > /dev/null; fi

changelog-new:
	$(CHANGIE) new

# Runs the shell tests for scripts/ (scripts/*_test.sh).
scripts-test:
	for t in scripts/*_test.sh; do "$$t" || exit 1; done

docs-lint:
	BASE_REF="$(BASE_REF)" scripts/docs-lint.sh --warn-only

api-compat:
	BASE_REF="$(BASE_REF)" scripts/api-compat.sh

# Runs the known-defect tests: tests of correct behaviour that fail today because of an open defect. They are
# excluded from `make ci` and `make test` (build tag knowndefect; docs checks via --known-defects). This target
# is expected to fail until every defect is fixed; when one is fixed, move its test into the regular suite.
# The Go runs use -v so that every test is listed with its PASS, FAIL or SKIP result: a fixed test stays
# visible (and ready to move) even while another test in the same package still fails.
known-defects:
	@rc=0; \
	go test -v -tags knowndefect -run '^TestKnownDefect_' -count=1 ./... || rc=1; \
	(cd bin/b2keys && go test -v -tags knowndefect -run '^TestKnownDefect_' -count=1 ./...) || rc=1; \
	scripts/docs-consistency_test.sh --known-defects || rc=1; \
	exit $$rc

# Runs the known-defect tests that only fail where int is 32 bits (they SKIP in `make known-defects` on a
# 64-bit platform). GOARCH=386 binaries run natively on a Linux amd64 host; they cannot run on macOS or arm64.
known-defects-32bit:
	GOARCH=386 go test -v -tags knowndefect -run '^TestKnownDefect_(ChunkOffsetMultipliedIn64Bit|BucketIndexReducedBeforeIntConversion)$$' -count=1 ./b2 ./x/window

# Holds a release while solvable security or quality findings are open (see RELEASE.md).
release-check:
	scripts/release-check.sh
