# gache [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![release](https://img.shields.io/github/release/kpango/gache.svg)](https://github.com/kpango/gache/releases/latest) [![CircleCI](https://circleci.com/gh/kpango/gache.svg?style=shield)](https://circleci.com/gh/kpango/gache) [![codecov](https://codecov.io/gh/kpango/gache/branch/master/graph/badge.svg)](https://codecov.io/gh/kpango/gache) [![Codacy Badge](https://api.codacy.com/project/badge/Grade/ac73fd76d01140a38c5650b9278bc971)](https://www.codacy.com/app/i.can.feel.gravity/gache?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=kpango/gache&amp;utm_campaign=Badge_Grade) [![Go Report Card](https://goreportcard.com/badge/github.com/kpango/gache)](https://goreportcard.com/report/github.com/kpango/gache) [![GoDoc](http://godoc.org/github.com/kpango/gache?status.svg)](http://godoc.org/github.com/kpango/gache) [![Join the chat at https://gitter.im/kpango/gache](https://badges.gitter.im/kpango/gache.svg)](https://gitter.im/kpango/gache?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)

gache is thinnest cache library for go application

## Requirement
Go 1.9

## Installation
```shell
go get github.com/kpango/gache
```

## Example
```go
	// data sets
	var (
		key1 = "key"
		key2 = 5050
		key3 = struct{}{}

		value1 = "value"
		value2 = 88888
		value3 = struct{}{}
	)

	// store cache default expire is 30 Seconds
	gache.Set(key1, value3)
	gache.Set(key2, value2)
	gache.Set(key3, value1)

	// load cache data
	v1, ok := gache.Get(key1)

	v2, ok := gache.Get(key2)

	v3, ok := gache.Get(key3)

```
## Benchmarks

[gache](https://github.com/kpango/gache) vs [normal map with lock](https://github.com/kpango/gache/blob/master/gache_bench_test.go#L13-L35) vs [bigcache](https://github.com/allegro/bigcache) vs [go-cache](https://github.com/patrickmn/go-cache) vs [gcache](https://github.com/bluele/gcache) vs [freecache](https://github.com/coocood/freecache) vs [gocache](https://github.com/hlts2/gocache)

![Bench](https://github.com/kpango/gache/raw/master/images/bench.png)

```
go test -count=5 -run=NONE -bench . -benchmem
goos: linux
goarch: amd64
pkg: github.com/kpango/gache
BenchmarkGache-4       	     200	   7302046 ns/op	  799992 B/op	   49999 allocs/op
BenchmarkGache-4       	     200	   7482927 ns/op	  799990 B/op	   49999 allocs/op
BenchmarkGache-4       	     200	   7564026 ns/op	  799988 B/op	   49999 allocs/op
BenchmarkGache-4       	     200	   7894300 ns/op	  799984 B/op	   49999 allocs/op
BenchmarkGache-4       	     200	   7796678 ns/op	  799984 B/op	   49999 allocs/op
BenchmarkGocache-4     	     200	   7747051 ns/op	  810844 B/op	   40102 allocs/op
BenchmarkGocache-4     	     200	   7875410 ns/op	  810345 B/op	   40102 allocs/op
BenchmarkGocache-4     	     200	   7977237 ns/op	  814397 B/op	   40103 allocs/op
BenchmarkGocache-4     	     200	   8040473 ns/op	  813389 B/op	   40103 allocs/op
BenchmarkGocache-4     	     200	   7947292 ns/op	  808809 B/op	   40101 allocs/op
BenchmarkMap-4         	     100	  10550466 ns/op	  332729 B/op	   20001 allocs/op
BenchmarkMap-4         	     100	  10878293 ns/op	  332759 B/op	   20001 allocs/op
BenchmarkMap-4         	     100	  10910154 ns/op	  332778 B/op	   20001 allocs/op
BenchmarkMap-4         	     100	  11974606 ns/op	  332740 B/op	   20001 allocs/op
BenchmarkMap-4         	     100	  11315843 ns/op	  332758 B/op	   20001 allocs/op
BenchmarkGoCache-4     	     100	  11662383 ns/op	  175705 B/op	   10002 allocs/op
BenchmarkGoCache-4     	     100	  11552430 ns/op	  175784 B/op	   10002 allocs/op
BenchmarkGoCache-4     	     100	  11526310 ns/op	  175742 B/op	   10002 allocs/op
BenchmarkGoCache-4     	     100	  10424240 ns/op	  175700 B/op	   10002 allocs/op
BenchmarkGoCache-4     	     100	  10588250 ns/op	  175732 B/op	   10002 allocs/op
BenchmarkGCacheLRU-4   	     100	  19290965 ns/op	 1971928 B/op	   60144 allocs/op
BenchmarkGCacheLRU-4   	     100	  17787053 ns/op	 1977772 B/op	   60162 allocs/op
BenchmarkGCacheLRU-4   	     100	  19028492 ns/op	 1978220 B/op	   60162 allocs/op
BenchmarkGCacheLRU-4   	     100	  17974750 ns/op	 1973850 B/op	   60152 allocs/op
BenchmarkGCacheLRU-4   	     100	  18830665 ns/op	 1976743 B/op	   60159 allocs/op
BenchmarkGCacheLFU-4   	     100	  19189776 ns/op	 1441237 B/op	   49995 allocs/op
BenchmarkGCacheLFU-4   	     100	  20143241 ns/op	 1442599 B/op	   50002 allocs/op
BenchmarkGCacheLFU-4   	     100	  20587852 ns/op	 1442078 B/op	   50001 allocs/op
BenchmarkGCacheLFU-4   	     100	  19949558 ns/op	 1442329 B/op	   50001 allocs/op
BenchmarkGCacheLFU-4   	     100	  20997721 ns/op	 1440370 B/op	   49991 allocs/op
BenchmarkGCacheARC-4   	      30	  54880697 ns/op	 3026273 B/op	   80396 allocs/op
BenchmarkGCacheARC-4   	      30	  56401137 ns/op	 3036504 B/op	   80430 allocs/op
BenchmarkGCacheARC-4   	      30	  53542137 ns/op	 3034153 B/op	   80420 allocs/op
BenchmarkGCacheARC-4   	      30	  54939956 ns/op	 3016683 B/op	   80353 allocs/op
BenchmarkGCacheARC-4   	      30	  55050945 ns/op	 3031923 B/op	   80419 allocs/op
BenchmarkFreeCache-4   	      50	  27326899 ns/op	126834447 B/op	   39939 allocs/op
BenchmarkFreeCache-4   	      50	  26745435 ns/op	126834401 B/op	   39939 allocs/op
BenchmarkFreeCache-4   	      50	  26885188 ns/op	126834403 B/op	   39939 allocs/op
BenchmarkFreeCache-4   	      50	  27817503 ns/op	126834396 B/op	   39939 allocs/op
BenchmarkFreeCache-4   	      50	  26268283 ns/op	126834394 B/op	   39939 allocs/op
BenchmarkBigCache-4    	      20	  65879200 ns/op	220661891 B/op	   30319 allocs/op
BenchmarkBigCache-4    	      20	  68852161 ns/op	220674293 B/op	   30322 allocs/op
BenchmarkBigCache-4    	      20	  66069864 ns/op	220660739 B/op	   30320 allocs/op
BenchmarkBigCache-4    	      20	  68671347 ns/op	220658764 B/op	   30320 allocs/op
BenchmarkBigCache-4    	      20	  67791004 ns/op	220680852 B/op	   30322 allocs/op
PASS
ok  	github.com/kpango/gache	83.782s
```

## Contribution
1. Fork it ( https://github.com/kpango/gache/fork )
2. Create your feature branch (git checkout -b my-new-feature)
3. Commit your changes (git commit -am 'Add some feature')
4. Push to the branch (git push origin my-new-feature)
5. Create new Pull Request

## Author
[kpango](https://github.com/kpango)

## LICENSE
gache released under MIT license, refer [LICENSE](https://github.com/kpango/gache/blob/master/LICENSE) file.
