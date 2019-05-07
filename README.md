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
go test -count=5 -run=NONE -bench . -benchmem
goos: darwin
goarch: amd64
pkg: github.com/kpango/gache
BenchmarkGacheWithSmallDataset-8       	10000000	       229 ns/op	     192 B/op	       8 allocs/op
BenchmarkGacheWithSmallDataset-8       	10000000	       228 ns/op	     192 B/op	       8 allocs/op
BenchmarkGacheWithSmallDataset-8       	10000000	       233 ns/op	     192 B/op	       8 allocs/op
BenchmarkGacheWithSmallDataset-8       	10000000	       230 ns/op	     192 B/op	       8 allocs/op
BenchmarkGacheWithSmallDataset-8       	10000000	       228 ns/op	     192 B/op	       8 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3266326 ns/op	  480003 B/op	   20000 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3435887 ns/op	  480003 B/op	   20000 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3293473 ns/op	  480003 B/op	   20000 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3320686 ns/op	  480008 B/op	   20000 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3306882 ns/op	  480003 B/op	   20000 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       342 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       353 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       349 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       345 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       344 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithBigDataset-8       	     500	   3769988 ns/op	  803860 B/op	   40049 allocs/op
BenchmarkGocacheWithBigDataset-8       	     500	   3662657 ns/op	  804242 B/op	   40052 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3693801 ns/op	  807375 B/op	   40087 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3662074 ns/op	  806655 B/op	   40083 allocs/op
BenchmarkGocacheWithBigDataset-8       	     500	   3682411 ns/op	  804629 B/op	   40052 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1255 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1264 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1301 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1265 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1269 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10038787 ns/op	  332771 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     200	   9950130 ns/op	  326347 B/op	   20000 allocs/op
BenchmarkMapWithBigDataset-8           	     200	  10025711 ns/op	  326395 B/op	   20000 allocs/op
BenchmarkMapWithBigDataset-8           	     200	   9913849 ns/op	  326375 B/op	   20000 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10089161 ns/op	  332739 B/op	   20001 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1488 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1493 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1500 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1524 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1496 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10172852 ns/op	  175826 B/op	   10003 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10196606 ns/op	  175719 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10155225 ns/op	  175730 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10080347 ns/op	  175687 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10139212 ns/op	  175739 B/op	   10002 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2447 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2481 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2463 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2458 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2449 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  20303907 ns/op	 1978406 B/op	   60159 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  20252786 ns/op	 1977566 B/op	   60158 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  20340804 ns/op	 1975780 B/op	   60155 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  20360024 ns/op	 1974747 B/op	   60147 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21916110 ns/op	 1973632 B/op	   60146 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3159 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3058 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3345 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3605 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3095 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	      50	  23091335 ns/op	 1439551 B/op	   49988 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  21456468 ns/op	 1439454 B/op	   49986 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  22155062 ns/op	 1439969 B/op	   49989 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  22007458 ns/op	 1439730 B/op	   49987 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  23379462 ns/op	 1440896 B/op	   49993 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3778 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  300000	      3550 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3144 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3208 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3124 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  65356559 ns/op	 2986013 B/op	   80239 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  68426599 ns/op	 2997542 B/op	   80283 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  67212658 ns/op	 2997620 B/op	   80287 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      20	  67104716 ns/op	 2992918 B/op	   80253 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  66066147 ns/op	 2996510 B/op	   80281 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1216 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1245 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1249 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1297 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1291 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	      50	  26189739 ns/op	126835570 B/op	   39940 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	      50	  22649865 ns/op	126835572 B/op	   39940 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  22460533 ns/op	126810825 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  22407424 ns/op	126810837 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  22559765 ns/op	126810840 B/op	   39937 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1419 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1314 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1369 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1412 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1342 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      20	  67048752 ns/op	218980016 B/op	   30322 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      20	  58592116 ns/op	218988724 B/op	   30321 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  49927797 ns/op	229230211 B/op	   30234 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  61055157 ns/op	229220825 B/op	   30233 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      20	  57389927 ns/op	219010363 B/op	   30323 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  200000	     16411 ns/op	    4379 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     12582 ns/op	    4394 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     22546 ns/op	    4361 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     20408 ns/op	    4405 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     26556 ns/op	    4462 B/op	      40 allocs/op
BenchmarkMCacheWithBigDataset-8        	      20	  56199562 ns/op	 4320073 B/op	   70002 allocs/op
BenchmarkMCacheWithBigDataset-8        	      30	  47394147 ns/op	 4670813 B/op	   73655 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  51949655 ns/op	 6237569 B/op	   89975 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  50295095 ns/op	 4739066 B/op	   74366 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  55176820 ns/op	 6239356 B/op	   89994 allocs/op
PASS
ok  	github.com/kpango/gache	232.025s
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
