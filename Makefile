BASE_BRANCH ?= main
BENCH_TESTS ?= .
BENCH_DIR ?= bench_results
WORKTREE_DIR ?= .worktree
CURRENT_BRANCH := $(shell git branch --show-current)
ifeq ($(CURRENT_BRANCH),)
CURRENT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
ifeq ($(CURRENT_BRANCH),HEAD)
CURRENT_BRANCH := $(shell git rev-parse --short HEAD)
endif
endif
SAFE_BRANCH := $(shell echo "$(CURRENT_BRANCH)" | tr '/' '-')
SAFE_BASE := $(shell echo "$(BASE_BRANCH)" | tr '/' '-')
XARGS_NO_RUN_IF_EMPTY := $(eval XARGS_NO_RUN_IF_EMPTY := $(shell xargs --version 2>/dev/null | head -1 | grep -qi gnu && echo -r))$(XARGS_NO_RUN_IF_EMPTY)

GO_VERSION := 1.26.1
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
	    $(ROOTDIR)/$(BENCH_DIR) \
	    $(ROOTDIR)/$(WORKTREE_DIR)

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

test:
	CGO_ENABLED=1 GO111MODULE=on go test -race -v $(go list ./... | rg -v vendor)

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

lint:
	golangci-lint run --config $(ROOTDIR)/.golangci.json --fix

.PHONY: perm
## set correct permissions for dirs and files
perm:
	find $(ROOTDIR) -type d -not -path "$(ROOTDIR)/.git*" -exec chmod -R 755 {} \;
	@if [ -f "$(ROOTDIR)/.gitfiles" ]; then \
		grep -vE '^\s*#' "$(ROOTDIR)/.gitfiles" | grep -v gitignore \
		| xargs $(XARGS_NO_RUN_IF_EMPTY) -I {} -P"$(CORES)" chmod 644 "{}"; \
	fi
	if [ -d "$(ROOTDIR)/.git" ]; then \
		chmod 750 "$(ROOTDIR)/.git"; \
		if [ -f "$(ROOTDIR)/.git/config" ]; then \
			chmod 644 "$(ROOTDIR)/.git/config"; \
		fi; \
	if [ -d "$(ROOTDIR)/.git/hooks" ]; then \
	find "$(ROOTDIR)/.git/hooks" -type f -exec chmod 755 {} \;; \
	fi; \
	fi
	if [ -f "$(ROOTDIR)/.gitignore" ]; then \
		chmod 644 "$(ROOTDIR)/.gitignore"; \
	fi
	if [ -f "$(ROOTDIR)/.gitattributes" ]; then \
		chmod 644 "$(ROOTDIR)/.gitattributes"; \
	fi


$(BENCH_DIR) $(WORKTREE_DIR):
	mkdir -p $@

$(WORKTREE_DIR)/$(BASE_BRANCH): | $(WORKTREE_DIR)
	git worktree remove -f $@ 2>/dev/null || true
	git worktree add $@ $(BASE_BRANCH)

sync-test-files: | $(WORKTREE_DIR)/$(BASE_BRANCH)
	@echo "Copying changed test files to $(BASE_BRANCH)..."
	@git diff --name-only $(BASE_BRANCH)...$(CURRENT_BRANCH) | grep -E '_test\.go$$' | while read -r file; do \
		mkdir -p "$(WORKTREE_DIR)/$(BASE_BRANCH)/$$(dirname "$$file")"; \
		if [ -f "$(ROOTDIR)/$$file" ]; then cp "$(ROOTDIR)/$$file" "$(WORKTREE_DIR)/$(BASE_BRANCH)/$$file"; fi; \
	done || true

bench-compare: deps $(BENCH_DIR) sync-test-files
	@trap 'echo "Cleaning up workspace..."; git worktree remove --force $(WORKTREE_DIR)/$(BASE_BRANCH) 2>/dev/null || true' EXIT; \
	if [ -z "$(CURRENT_BRANCH)" ] || [ "$(CURRENT_BRANCH)" = "$(BASE_BRANCH)" ]; then \
		echo "Must be on a branch other than $(BASE_BRANCH) to compare." && exit 1; \
	fi; \
	echo "Comparing benchmarks: $(BASE_BRANCH) vs $(CURRENT_BRANCH)"; \
	echo "Running benchmarks on $(CURRENT_BRANCH)..."; \
	go test -count=5 -timeout=30m -run=NONE -bench=$(BENCH_TESTS) -benchmem ./... | tee $(ROOTDIR)/$(BENCH_DIR)/$(SAFE_BRANCH).log; \
	echo "Running benchmarks on $(BASE_BRANCH)..."; \
	go test -C $(WORKTREE_DIR)/$(BASE_BRANCH) -count=5 -timeout=30m -run=NONE -bench=$(BENCH_TESTS) -benchmem ./... | tee $(ROOTDIR)/$(BENCH_DIR)/$(SAFE_BASE).log; \
	echo "Comparing results..."; \
	command -v benchstat > /dev/null || (echo "Installing benchstat..." && go install golang.org/x/perf/cmd/benchstat@latest); \
	$$(go env GOPATH)/bin/benchstat $(ROOTDIR)/$(BENCH_DIR)/$(SAFE_BASE).log $(ROOTDIR)/$(BENCH_DIR)/$(SAFE_BRANCH).log > $(ROOTDIR)/$(BENCH_DIR)/benchstat-$(SAFE_BASE)-$(SAFE_BRANCH); \
	cat $(ROOTDIR)/$(BENCH_DIR)/benchstat-$(SAFE_BASE)-$(SAFE_BRANCH)
