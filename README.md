# gache [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![release](https://img.shields.io/github/release/kpango/gache.svg)](https://github.com/kpango/gache/releases/latest) [![CircleCI](https://circleci.com/gh/kpango/gache.svg?style=shield)](https://circleci.com/gh/kpango/gache) [![Codacy Badge](https://api.codacy.com/project/badge/Grade/ac73fd76d01140a38c5650b9278bc971)](https://www.codacy.com/app/i.can.feel.gravity/gache?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=kpango/gache&amp;utm_campaign=Badge_Grade) [![Go Report Card](https://goreportcard.com/badge/github.com/kpango/gache)](https://goreportcard.com/report/github.com/kpango/gache) [![GoDoc](http://godoc.org/github.com/kpango/gache?status.svg)](http://godoc.org/github.com/kpango/gache) [![Join the chat at https://gitter.im/kpango/gache](https://badges.gitter.im/kpango/gache.svg)](https://gitter.im/kpango/gache?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge) [![DepShield Badge](https://depshield.sonatype.org/badges/kpango/gache/depshield.svg)](https://depshield.github.io) [![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fkpango%2Fgache.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Fkpango%2Fgache?ref=badge_shield)

gache is thinnest cache library for go application

## Requirement
Go 1.11

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


	gache.Write(context.Background(), glg.FileWriter("./gache-sample.gdb", 0755))
	gache.New().SetDefaultExpire(time.Minute).Read(glg.FileWriter("./gache-sample.gdb", 0755))
```
## Benchmarks

[gache](https://github.com/kpango/gache) vs [gocache](https://github.com/hlts2/gocache) vs [normal map with lock](https://github.com/kpango/gache/blob/master/gache_bench_test.go#L13-L35) vs [go-cache](https://github.com/patrickmn/go-cache) vs [gcache](https://github.com/bluele/gcache) vs [freecache](https://github.com/coocood/freecache) vs [bigcache](https://github.com/allegro/bigcache) vs [go-mcache](https://github.com/OrlovEvgeny/go-mcache)


```ltsv
go test -count=1 -run=NONE -bench . -benchmem
goos: darwin
goarch: amd64
pkg: github.com/kpango/gache
BenchmarkGacheWithSmallDataset-8       	 10000000	       217 ns/op	     192 B/op	       8 allocs/op
BenchmarkGacheWithBigDataset-8         	      500	   3367835 ns/op	  480004 B/op	   20000 allocs/op
BenchmarkGocacheWithSmallDataset-8     	  5000000	       371 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithBigDataset-8       	      300	   3913257 ns/op	  809037 B/op	   40092 allocs/op
BenchmarkFastCacheWithSmallDataset-8   	  1000000	      1061 ns/op	      40 B/op	       4 allocs/op
BenchmarkFastCacheWithBigDataset-8     	       50	  24633452 ns/op	126797919 B/op	   39986 allocs/op
BenchmarkMapWithSmallDataset-8         	  1000000	      1381 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithBigDataset-8           	      100	  10477737 ns/op	  332664 B/op	   20001 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	  1000000	      1226 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	      100	  20914503 ns/op	126812350 B/op	   39938 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	  1000000	      1416 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithBigDataset-8      	       20	  54483710 ns/op	219245782 B/op	   30273 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	  1000000	      1506 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithBigDataset-8       	      100	  10465906 ns/op	  175742 B/op	   10002 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	   500000	      2450 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	      100	  21327392 ns/op	 1976773 B/op	   60154 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	   500000	      2953 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	      100	  22398865 ns/op	 1440065 B/op	   49989 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	   500000	      3121 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	       20	  66966396 ns/op	 3010356 B/op	   80309 allocs/op
BenchmarkMCacheWithSmallDataset-8      	   100000	     14009 ns/op	    4358 B/op	      40 allocs/op
BenchmarkMCacheWithBigDataset-8        	       30	  37309669 ns/op	11098881 B/op	  100002 allocs/op
PASS
ok  	github.com/kpango/gache	37.067s
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


[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fkpango%2Fgache.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fkpango%2Fgache?ref=badge_large)
