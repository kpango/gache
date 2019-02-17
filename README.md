# gache [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![release](https://img.shields.io/github/release/kpango/gache.svg)](https://github.com/kpango/gache/releases/latest) [![CircleCI](https://circleci.com/gh/kpango/gache.svg?style=shield)](https://circleci.com/gh/kpango/gache) [![codecov](https://codecov.io/gh/kpango/gache/branch/master/graph/badge.svg)](https://codecov.io/gh/kpango/gache) [![Codacy Badge](https://api.codacy.com/project/badge/Grade/ac73fd76d01140a38c5650b9278bc971)](https://www.codacy.com/app/i.can.feel.gravity/gache?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=kpango/gache&amp;utm_campaign=Badge_Grade) [![Go Report Card](https://goreportcard.com/badge/github.com/kpango/gache)](https://goreportcard.com/report/github.com/kpango/gache) [![GoDoc](http://godoc.org/github.com/kpango/gache?status.svg)](http://godoc.org/github.com/kpango/gache) [![Join the chat at https://gitter.im/kpango/gache](https://badges.gitter.im/kpango/gache.svg)](https://gitter.im/kpango/gache?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge) [![DepShield Badge](https://depshield.sonatype.org/badges/kpango/gache/depshield.svg)](https://depshield.github.io)
[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fkpango%2Fgache.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Fkpango%2Fgache?ref=badge_shield)

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

```
## Benchmarks

[gache](https://github.com/kpango/gache) vs [gocache](https://github.com/hlts2/gocache) vs [normal map with lock](https://github.com/kpango/gache/blob/master/gache_bench_test.go#L13-L35) vs [go-cache](https://github.com/patrickmn/go-cache) vs [gcache](https://github.com/bluele/gcache) vs [freecache](https://github.com/coocood/freecache) vs [bigcache](https://github.com/allegro/bigcache) vs [go-mcache](https://github.com/OrlovEvgeny/go-mcache)


```ltsv
go test -count=5 -run=NONE -bench . -benchmem
goos: linux
goarch: amd64
pkg: github.com/kpango/gache
BenchmarkGacheWithSmallDataset-8       	 5000000	       312 ns/op	     320 B/op	      20 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       324 ns/op	     320 B/op	      20 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       322 ns/op	     320 B/op	      20 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       310 ns/op	     320 B/op	      20 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       318 ns/op	     320 B/op	      20 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3565117 ns/op	  799994 B/op	   49999 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3607673 ns/op	  799990 B/op	   49999 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3549472 ns/op	  799991 B/op	   49999 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3566316 ns/op	  799985 B/op	   49999 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3711345 ns/op	  799991 B/op	   49999 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       364 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       388 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       379 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       385 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       386 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4135093 ns/op	  807597 B/op	   40087 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3991564 ns/op	  806232 B/op	   40082 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4159144 ns/op	  806202 B/op	   40083 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4129725 ns/op	  806425 B/op	   40083 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4304676 ns/op	  806656 B/op	   40085 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 2000000	       992 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1245 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1306 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 2000000	      1001 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 2000000	       987 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10038628 ns/op	  175632 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10465542 ns/op	  175735 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10530687 ns/op	  175683 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10615931 ns/op	  175756 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10450110 ns/op	  175727 B/op	   10002 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1291 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1205 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1187 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1207 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1168 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10550628 ns/op	  332751 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10560827 ns/op	  332710 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10506975 ns/op	  332773 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10535671 ns/op	  332781 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10878696 ns/op	  332753 B/op	   20001 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1144 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1147 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1167 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1112 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1095 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  22504867 ns/op	126810340 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  23726856 ns/op	126810365 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  23011774 ns/op	126834636 B/op	   39940 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  22741728 ns/op	126810328 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  23413177 ns/op	126810342 B/op	   39937 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1333 ns/op	     419 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1210 ns/op	     419 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1201 ns/op	     419 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1197 ns/op	     419 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1205 ns/op	     419 B/op	       8 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      20	  68395660 ns/op	220166154 B/op	   30322 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  65040949 ns/op	229025953 B/op	   30232 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  55505040 ns/op	229043814 B/op	   30235 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  47414913 ns/op	229044831 B/op	   30234 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  46179392 ns/op	229033484 B/op	   30233 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	 1000000	      2092 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	 1000000	      2129 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	 1000000	      2132 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	 1000000	      2063 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	 1000000	      2060 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  18894625 ns/op	 1976380 B/op	   60159 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  19425641 ns/op	 1974108 B/op	   60152 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  19562765 ns/op	 1973393 B/op	   60150 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  18960524 ns/op	 1976306 B/op	   60159 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  18512585 ns/op	 1976201 B/op	   60159 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      2991 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      2716 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      2553 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      4862 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      2407 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  19530066 ns/op	 1439466 B/op	   49986 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  19622034 ns/op	 1440294 B/op	   49992 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  19543709 ns/op	 1440204 B/op	   49991 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  19972479 ns/op	 1439748 B/op	   49987 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  19542925 ns/op	 1440201 B/op	   49990 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      2772 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      2722 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      2729 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      2734 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      2800 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  58675680 ns/op	 2992860 B/op	   80283 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      20	  57168284 ns/op	 2997710 B/op	   80310 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  58028858 ns/op	 2987443 B/op	   80266 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  57437412 ns/op	 3011644 B/op	   80348 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  57491607 ns/op	 2997677 B/op	   80304 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  200000	     11660 ns/op	    4380 B/op	      39 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  200000	     18256 ns/op	    4387 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  200000	     26122 ns/op	    4335 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  200000	     43760 ns/op	    4426 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  200000	      9039 ns/op	    1762 B/op	      28 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  36737005 ns/op	 4858355 B/op	   74408 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  66796372 ns/op	 6899223 B/op	   86612 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  87165588 ns/op	 8310018 B/op	   89585 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  41070226 ns/op	 4448142 B/op	   70000 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  40866029 ns/op	 4320019 B/op	   70000 allocs/op
PASS
ok  	github.com/kpango/gache	241.014s
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