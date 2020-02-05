GO_VERSION:=$(shell go version)

.PHONY: all clean bench bench-all profile lint test contributors update install

all: clean install lint test bench

clean:
	go clean ./...
	go clean -modcache
	rm -rf ./*.log
	rm -rf ./*.svg
	rm -rf ./go.*
	rm -rf pprof
	rm -rf bench
	rm -rf vendor

bench: clean init
	sleep 3
	go test -count=1 -run=NONE -bench . -benchmem

init:
	GO111MODULE=on go mod init
	GO111MODULE=on go mod vendor

profile: clean init
	rm -rf bench
	mkdir bench
	mkdir pprof
	\
	go test -count=10 -run=NONE -bench=BenchmarkGacheWithBigDataset -benchmem -o pprof/gache-test.bin -cpuprofile pprof/cpu-gache.out -memprofile pprof/mem-gache.out
	go tool pprof --svg pprof/gache-test.bin pprof/cpu-gache.out > cpu-gache.svg
	go tool pprof --svg pprof/gache-test.bin pprof/mem-gache.out > mem-gache.svg
	\
	go test -count=10 -run=NONE -bench=BenchmarkGocacheWithBigDataset -benchmem -o pprof/gocache-test.bin -cpuprofile pprof/cpu-gocache.out -memprofile pprof/mem-gocache.out
	go tool pprof --svg pprof/gocache-test.bin pprof/mem-gocache.out > mem-gocache.svg
	go tool pprof --svg pprof/gocache-test.bin pprof/cpu-gocache.out > cpu-gocache.svg
	\
	go test -count=10 -run=NONE -bench=BenchmarkMapWithBigDataset -benchmem -o pprof/test.bin -cpuprofile pprof/cpu-def.out -memprofile pprof/mem-def.out
	go tool pprof --svg pprof/test.bin pprof/mem-def.out > mem-def.svg
	go tool pprof --svg pprof/test.bin pprof/cpu-def.out > cpu-def.svg
	\
	mv ./*.svg bench/

profile-web:
	go tool pprof -http=":6061" \
		pprof/gache-test.bin \
		pprof/cpu-gache.out &
	go tool pprof -http=":6062" \
		pprof/gocache-test.bin \
		pprof/mem-gocache.out

lint:
	gometalinter --enable-all . | rg -v comment

test:
	GO111MODULE=on go test --race -v $(go list ./... | rg -v vendor)

contributors:
	git log --format='%aN <%aE>' | sort -fu > CONTRIBUTORS

run:
	go run example/main.go
