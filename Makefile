BASE_BRANCH ?= main
BENCH_TESTS ?= .
BENCH_DIR ?= bench_results

GO_VERSION := 1.26.0
GOPATH := $(eval GOPATH := $(shell go env GOPATH))$(GOPATH)
GOLINES_MAX_WIDTH     ?= 200

ROOTDIR = $(eval ROOTDIR := $(or $(shell git rev-parse --show-toplevel), $(PWD)))$(ROOTDIR)

.PHONY: all clean bench bench-all profile lint test contributors update install

all: clean install lint test bench

clean:
	go clean ./...
	go clean -modcache
	rm -rf \
	    $(ROOTDIR)/*.log \
	    $(ROOTDIR)/*.svg \
	    $(ROOTDIR)/go.mod \
	    $(ROOTDIR)/go.sum \
	    $(ROOTDIR)/pprof \
	    $(ROOTDIR)/bench \
	    $(ROOTDIR)/vendor \
	    $(ROOTDIR)/$(BENCH_DIR)

.PHONY: deps
## install Go package dependencies
deps: \
	clean \
	init
	head -n -1 $(ROOTDIR)/go.mod.default | awk 'NR>=6 && $$0 !~ /(upgrade|latest|master|main)/' | sort
	rm -rf $(ROOTDIR)/vendor \
	    $(ROOTDIR)/go.sum \
	    $(ROOTDIR)/go.mod 2>/dev/null
	cp $(ROOTDIR)/go.mod.default $(ROOTDIR)/go.mod
	sed -E "s/^go [0-9]+\.[0-9]+(\.[0-9]+)?/go $(GO_VERSION)/; s/#.*//" $(ROOTDIR)/go.mod > $(ROOTDIR)/go.mod.tmp
	mv $(ROOTDIR)/go.mod.tmp $(ROOTDIR)/go.mod
	GOTOOLCHAIN=go$(GO_VERSION) go mod tidy
	GOTOOLCHAIN=go$(GO_VERSION) go get -u all 2>/dev/null || true
	GOTOOLCHAIN=go$(GO_VERSION) go get -u ./... 2>/dev/null || true

bench: deps
	sleep 3
	go test -count=1 -timeout=30m -run=NONE -bench . -benchmem

bench/gache: deps
	sleep 3
	go test -count=1 -timeout=30m -run=NONE -bench=BenchmarkGache -benchmem

init:
	GO111MODULE=on go mod init github.com/kpango/gache/v2
	GO111MODULE=on go mod tidy
	go get -u ./...

profile: deps
	rm -rf bench
	mkdir bench
	mkdir pprof
	\
	# go test -count=3 -timeout=30m -run=NONE -bench=BenchmarkChangeOutAllInt_gache -benchmem -o pprof/gache-test.bin -cpuprofile pprof/cpu-gache.out -memprofile pprof/mem-gache.out
	go test -count=3 -timeout=30m -run=NONE -bench=BenchmarkGache -benchmem -o pprof/gache-test.bin -cpuprofile pprof/cpu-gache.out -memprofile pprof/mem-gache.out
	go tool pprof --svg pprof/gache-test.bin pprof/cpu-gache.out > cpu-gache.svg
	go tool pprof --svg pprof/gache-test.bin pprof/mem-gache.out > mem-gache.svg
	\
	mv ./*.svg bench/

profile-gache: deps
	rm -rf bench
	mkdir bench
	mkdir pprof
	\
	go test -count=3 -timeout=30m -run=NONE -bench=BenchmarkGache -benchmem -o pprof/gache-test.bin -cpuprofile pprof/cpu-gache.out -memprofile pprof/mem-gache.out
	go tool pprof --svg pprof/gache-test.bin pprof/cpu-gache.out > cpu-gache.svg
	go tool pprof --svg pprof/gache-test.bin pprof/mem-gache.out > mem-gache.svg
	\
	mv ./*.svg bench/

profile-web-cpu:
	go tool pprof -http=":6061" \
		pprof/gache-test.bin \
		pprof/cpu-gache.out

profile-web-mem:
	go tool pprof -http=":6062" \
		pprof/gache-test.bin \
		pprof/mem-gache.out

lint:
	gometalinter --enable-all . | rg -v comment

test:
	CGO_ENABLED=1 GO111MODULE=on go test --race -v $(go list ./... | rg -v vendor)

contributors:
	git log --format='%aN <%aE>' | sort -fu > CONTRIBUTORS

run:
	go run example/main.go

format:
	find ./ -type d -name .git -prune -o -type f -regex '.*[^\.pb]\.go' -print | xargs $(GOPATH)/bin/golines -w -m $(GOLINES_MAX_WIDTH)
	find ./ -type d -name .git -prune -o -type f -regex '.*[^\.pb]\.go' -print | xargs $(GOPATH)/bin/gofumpt -w
	find ./ -type d -name .git -prune -o -type f -regex '.*[^\.pb]\.go' -print | xargs $(GOPATH)/bin/strictgoimports -w
	find ./ -type d -name .git -prune -o -type f -regex '.*\.go' -print | xargs $(GOPATH)/bin/goimports -w
	go fix ./...

bench-compare: deps
	@CURRENT_BRANCH=$$(git branch --show-current); \
	SAFE_BRANCH=$$(echo "$$CURRENT_BRANCH" | tr '/' '-'); \
	SAFE_BASE=$$(echo "$(BASE_BRANCH)" | tr '/' '-'); \
	if [ -z "$$CURRENT_BRANCH" ] || [ "$$CURRENT_BRANCH" = "$(BASE_BRANCH)" ]; then \
		echo "Must be on a branch other than $(BASE_BRANCH) to compare." && exit 1; \
	fi; \
	echo "Comparing benchmarks: $(BASE_BRANCH) vs $$CURRENT_BRANCH"; \
	mkdir -p $(ROOTDIR)/$(BENCH_DIR); \
	echo "Running benchmarks on $$CURRENT_BRANCH..."; \
	go test -count=5 -timeout=30m -run=NONE -bench=$(BENCH_TESTS) -benchmem ./... | tee $(ROOTDIR)/$(BENCH_DIR)/$$SAFE_BRANCH.log; \
	echo "Setting up workspace for $(BASE_BRANCH)..."; \
	TEMP_DIR=$$(mktemp -d); \
	WORKTREE_DIR="$$TEMP_DIR/worktree"; \
	trap 'echo "Cleaning up workspace..."; git worktree remove --force $$WORKTREE_DIR 2>/dev/null || true; rm -rf $$TEMP_DIR' EXIT; \
	git worktree add $$WORKTREE_DIR $(BASE_BRANCH); \
	echo "Copying changed test files to $(BASE_BRANCH)..."; \
	git diff --name-only $(BASE_BRANCH)...$$CURRENT_BRANCH | grep -E '_test\.go$$' | while read -r file; do \
		mkdir -p "$$WORKTREE_DIR/$$(dirname "$$file")"; \
		cp "$(ROOTDIR)/$$file" "$$WORKTREE_DIR/$$file"; \
	done || true; \
	echo "Running benchmarks on $(BASE_BRANCH)..."; \
	(cd $$WORKTREE_DIR && go test -count=5 -timeout=30m -run=NONE -bench=$(BENCH_TESTS) -benchmem ./... | tee $(ROOTDIR)/$(BENCH_DIR)/$$SAFE_BASE.log); \
	echo "Comparing results..."; \
	if ! command -v benchstat > /dev/null; then \
		echo "Installing benchstat..." && go install golang.org/x/perf/cmd/benchstat@latest; \
	fi; \
	$$(go env GOPATH)/bin/benchstat $(ROOTDIR)/$(BENCH_DIR)/$$SAFE_BASE.log $(ROOTDIR)/$(BENCH_DIR)/$$SAFE_BRANCH.log > $(ROOTDIR)/$(BENCH_DIR)/benchstat-$$SAFE_BASE-$$SAFE_BRANCH; \
	cat $(ROOTDIR)/$(BENCH_DIR)/benchstat-$$SAFE_BASE-$$SAFE_BRANCH
