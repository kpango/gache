GO_VERSION:=$(shell go version)

.PHONY: all clean bench bench-all profile lint test contributors update install


GOPATH := $(eval GOPATH := $(shell go env GOPATH))$(GOPATH)
GOLINES_MAX_WIDTH     ?= 200

all: clean install lint test bench

clean:
	go clean ./...
	go clean -modcache
	rm -rf ./*.log \
	    ./*.svg \
	    ./go.* \
	    pprof \
	    bench \
	    vendor

bench: clean init
	sleep 3
	go test -count=1 -timeout=30m -run=NONE -bench . -benchmem

init:
	GO111MODULE=on go mod init github.com/kpango/gache/v2
	GO111MODULE=on go mod tidy
	go get -u ./...

profile: clean init
	rm -rf bench
	mkdir bench
	mkdir pprof
	\
	# go test -count=3 -timeout=30m -run=NONE -bench=BenchmarkChangeOutAllInt_gache -benchmem -o pprof/gache-test.bin -cpuprofile pprof/cpu-gache.out -memprofile pprof/mem-gache.out
	go test -count=3 -timeout=30m -run=NONE -bench=BenchmarkGacheSetBigDataWithTTL -benchmem -o pprof/gache-test.bin -cpuprofile pprof/cpu-gache.out -memprofile pprof/mem-gache.out
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
	CGO_ENABLED=1 GO111MODULE=on GOEXPERIMENT=noswissmap go test --race -v $(go list ./... | rg -v vendor)
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
