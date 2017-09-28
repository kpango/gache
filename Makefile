GO_VERSION:=$(shell go version)

.PHONY: all clean bench bench-all profile lint test contributors update install

all: clean install lint test bench

clean:
	go clean ./...
	rm -rf pprof
	rm -rf vendor

bench:
	go test -count=5 -run=NONE -bench . -benchmem

bench-all:
	sh ./bench.sh

profile:
	rm -rf bench
	mkdir bench
	go test -count=10 -run=NONE -bench . -benchmem -o pprof/test.bin -cpuprofile pprof/cpu.out -memprofile pprof/mem.out
	go tool pprof --svg pprof/test.bin pprof/mem.out > mem.svg
	go tool pprof --svg pprof/test.bin pprof/cpu.out > cpu.svg
	rm -rf pprof
	go test -count=10 -run=NONE -bench=BenchmarkGache -benchmem -o pprof/test.bin -cpuprofile pprof/cpu-gache.out -memprofile pprof/mem-gache.out
	go tool pprof --svg pprof/test.bin pprof/cpu-gache.out > cpu-gache.svg
	go tool pprof --svg pprof/test.bin pprof/mem-gache.out > mem-gache.svg
	rm -rf pprof
	go test -count=10 -run=NONE -bench=BenchmarkMap -benchmem -o pprof/test.bin -cpuprofile pprof/cpu-def.out -memprofile pprof/mem-def.out
	go tool pprof --svg pprof/test.bin pprof/mem-def.out > mem-def.svg
	go tool pprof --svg pprof/test.bin pprof/cpu-def.out > cpu-def.svg
	rm -rf pprof
	mv ./*.svg bench/

lint:
	gometalinter --enable-all . | rg -v comment

test:
	go test -v $(go list ./... | rg -v vendor)

contributors:
	git log --format='%aN <%aE>' | sort -fu > CONTRIBUTORS

update:
	glide upgrade
	glide update
