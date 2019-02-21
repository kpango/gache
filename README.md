# gache [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![release](https://img.shields.io/github/release/kpango/gache.svg)](https://github.com/kpango/gache/releases/latest) [![CircleCI](https://circleci.com/gh/kpango/gache.svg?style=shield)](https://circleci.com/gh/kpango/gache) [![codecov](https://codecov.io/gh/kpango/gache/branch/master/graph/badge.svg)](https://codecov.io/gh/kpango/gache) [![Codacy Badge](https://api.codacy.com/project/badge/Grade/ac73fd76d01140a38c5650b9278bc971)](https://www.codacy.com/app/i.can.feel.gravity/gache?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=kpango/gache&amp;utm_campaign=Badge_Grade) [![Go Report Card](https://goreportcard.com/badge/github.com/kpango/gache)](https://goreportcard.com/report/github.com/kpango/gache) [![GoDoc](http://godoc.org/github.com/kpango/gache?status.svg)](http://godoc.org/github.com/kpango/gache) [![Join the chat at https://gitter.im/kpango/gache](https://badges.gitter.im/kpango/gache.svg)](https://gitter.im/kpango/gache?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge) [![DepShield Badge](https://depshield.sonatype.org/badges/kpango/gache/depshield.svg)](https://depshield.github.io) [![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fkpango%2Fgache.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Fkpango%2Fgache?ref=badge_shield)

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
BenchmarkGacheWithSmallDataset-8       	 5000000	       291 ns/op	     320 B/op	      16 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       295 ns/op	     320 B/op	      16 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       298 ns/op	     320 B/op	      16 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       294 ns/op	     320 B/op	      16 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       299 ns/op	     320 B/op	      16 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3452521 ns/op	  799990 B/op	   39999 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3445126 ns/op	  799985 B/op	   39999 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3444726 ns/op	  799991 B/op	   39999 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3463403 ns/op	  799991 B/op	   39999 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3494554 ns/op	  799985 B/op	   39999 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       340 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       338 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       342 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       345 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 5000000	       342 ns/op	     320 B/op	      16 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3634880 ns/op	  803100 B/op	   40047 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3617405 ns/op	  804285 B/op	   40050 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3728884 ns/op	  806466 B/op	   40057 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3753736 ns/op	  809177 B/op	   40092 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3763500 ns/op	  808783 B/op	   40094 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1433 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1376 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1352 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1340 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1338 ns/op	     128 B/op	       8 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10181335 ns/op	  332736 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10135521 ns/op	  332758 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10147527 ns/op	  332748 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10191556 ns/op	  332700 B/op	   20001 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10203600 ns/op	  332779 B/op	   20001 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1548 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1514 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1507 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1503 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1502 ns/op	      64 B/op	       4 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10247210 ns/op	  175679 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10286331 ns/op	  175747 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10341409 ns/op	  175810 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10285768 ns/op	  175775 B/op	   10002 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10285459 ns/op	  175706 B/op	   10002 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2482 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2463 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2506 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2505 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2518 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21187149 ns/op	 1976314 B/op	   60152 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21365157 ns/op	 1974912 B/op	   60146 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21295620 ns/op	 1971798 B/op	   60136 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  20833879 ns/op	 1975621 B/op	   60147 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  20802686 ns/op	 1976400 B/op	   60151 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3081 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3023 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      2918 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      2928 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      2894 ns/op	     512 B/op	      20 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  21955102 ns/op	 1439551 B/op	   49986 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  21821424 ns/op	 1440684 B/op	   49992 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  21668914 ns/op	 1439789 B/op	   49987 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  21698323 ns/op	 1440822 B/op	   49993 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  21813723 ns/op	 1440316 B/op	   49990 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3198 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3069 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3090 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3095 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3076 ns/op	     320 B/op	      16 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  61898429 ns/op	 3002668 B/op	   80286 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  63439372 ns/op	 2984606 B/op	   80233 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  62344244 ns/op	 3001549 B/op	   80292 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  63272118 ns/op	 2991984 B/op	   80250 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  62868651 ns/op	 3012101 B/op	   80355 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1221 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1216 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1212 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1215 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1212 ns/op	      26 B/op	       4 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  19999670 ns/op	126810752 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  19969249 ns/op	126810741 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  20030303 ns/op	126810746 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  20055135 ns/op	126810783 B/op	   39937 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  20039316 ns/op	126810721 B/op	   39937 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1409 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1303 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1281 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1322 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1282 ns/op	     418 B/op	       8 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      20	  64710802 ns/op	218923337 B/op	   30322 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  81748764 ns/op	228873497 B/op	   30236 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  60661622 ns/op	228847402 B/op	   30232 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  47265941 ns/op	228855801 B/op	   30232 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  43173717 ns/op	228862094 B/op	   30234 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  200000	     13406 ns/op	    4385 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  200000	     21958 ns/op	    4380 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     30215 ns/op	    4393 B/op	      40 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     22808 ns/op	    2102 B/op	      31 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     19290 ns/op	    2496 B/op	      36 allocs/op
BenchmarkMCacheWithBigDataset-8        	      20	  79083650 ns/op	 6239998 B/op	   90001 allocs/op
BenchmarkMCacheWithBigDataset-8        	      30	  76086004 ns/op	 6240040 B/op	   90001 allocs/op
BenchmarkMCacheWithBigDataset-8        	      30	 106160223 ns/op	 6240044 B/op	   90001 allocs/op
BenchmarkMCacheWithBigDataset-8        	      30	  49614440 ns/op	 5966882 B/op	   85998 allocs/op
BenchmarkMCacheWithBigDataset-8        	      30	  51878904 ns/op	 5781447 B/op	   85224 allocs/op
PASS
ok  	github.com/kpango/gache	249.928s
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
