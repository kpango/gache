# gache [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![release](https://img.shields.io/github/release/kpango/gache.svg)](https://github.com/kpango/gache/releases/latest) [![CircleCI](https://circleci.com/gh/kpango/gache.svg?style=shield)](https://circleci.com/gh/kpango/gache) [![codecov](https://codecov.io/gh/kpango/gache/branch/master/graph/badge.svg)](https://codecov.io/gh/kpango/gache) [![Codacy Badge](https://api.codacy.com/project/badge/Grade/ac73fd76d01140a38c5650b9278bc971)](https://www.codacy.com/app/i.can.feel.gravity/gache?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=kpango/gache&amp;utm_campaign=Badge_Grade) [![Go Report Card](https://goreportcard.com/badge/github.com/kpango/gache)](https://goreportcard.com/report/github.com/kpango/gache) [![GoDoc](http://godoc.org/github.com/kpango/gache?status.svg)](http://godoc.org/github.com/kpango/gache) [![Join the chat at https://gitter.im/kpango/gache](https://badges.gitter.im/kpango/gache.svg)](https://gitter.im/kpango/gache?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge) [![DepShield Badge](https://depshield.sonatype.org/badges/kpango/gache/depshield.svg)](https://depshield.github.io)

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

[gache](https://github.com/kpango/gache) vs [gocache](https://github.com/hlts2/gocache) vs [normal map with lock](https://github.com/kpango/gache/blob/master/gache_bench_test.go#L13-L35) vs [go-cache](https://github.com/patrickmn/go-cache) vs [gcache](https://github.com/bluele/gcache) vs [freecache](https://github.com/coocood/freecache) vs [bigcache](https://github.com/allegro/bigcache) vs [go-mcache](https://github.com/OrlovEvgeny/go-mcache)


```
go test -count=5 -run=NONE -bench . -benchmem
goos: darwin
goarch: amd64
pkg: github.com/kpango/gache
BenchmarkGacheWithSmallDataset-8       	 3000000	       402 ns/op	     363 B/op	      21 allocs/op
BenchmarkGacheWithSmallDataset-8       	 3000000	       419 ns/op	     364 B/op	      21 allocs/op
BenchmarkGacheWithSmallDataset-8       	 3000000	       398 ns/op	     360 B/op	      21 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       408 ns/op	     363 B/op	      21 allocs/op
BenchmarkGacheWithSmallDataset-8       	 5000000	       404 ns/op	     362 B/op	      21 allocs/op
BenchmarkGacheWithBigDataset-8         	     300	   3952278 ns/op	 1263755 B/op	   64491 allocs/op
BenchmarkGacheWithBigDataset-8         	     300	   4413116 ns/op	 1290176 B/op	   65317 allocs/op
BenchmarkGacheWithBigDataset-8         	     300	   3918556 ns/op	 1270564 B/op	   64704 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3977327 ns/op	 1260602 B/op	   64393 allocs/op
BenchmarkGacheWithBigDataset-8         	     300	   3977146 ns/op	 1270649 B/op	   64707 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 3000000	       481 ns/op	     375 B/op	      17 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 3000000	       518 ns/op	     374 B/op	      17 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 3000000	       492 ns/op	     374 B/op	      17 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 3000000	       505 ns/op	     374 B/op	      17 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 3000000	       528 ns/op	     378 B/op	      17 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4228641 ns/op	 1299230 B/op	   55465 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4267744 ns/op	 1320021 B/op	   56057 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4496506 ns/op	 1303386 B/op	   55535 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4227252 ns/op	 1281210 B/op	   54840 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   4428165 ns/op	 1336318 B/op	   56472 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1495 ns/op	     426 B/op	      17 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1491 ns/op	     429 B/op	      17 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1475 ns/op	     428 B/op	      17 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1462 ns/op	     426 B/op	      17 allocs/op
BenchmarkMapWithSmallDataset-8         	 1000000	      1463 ns/op	     434 B/op	      17 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10694528 ns/op	 2637553 B/op	   92025 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10981050 ns/op	 2673483 B/op	   93149 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10711187 ns/op	 2588318 B/op	   90489 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10719385 ns/op	 2595016 B/op	   90696 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  11112614 ns/op	 2607860 B/op	   91097 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1598 ns/op	     395 B/op	      14 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1617 ns/op	     377 B/op	      13 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1617 ns/op	     387 B/op	      14 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1645 ns/op	     399 B/op	      14 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	 1000000	      1639 ns/op	     399 B/op	      14 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  11059278 ns/op	 2486459 B/op	   82213 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10818612 ns/op	 2477070 B/op	   81918 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10735781 ns/op	 2487358 B/op	   82239 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  11110248 ns/op	 2506247 B/op	   82830 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10750619 ns/op	 2466763 B/op	   81597 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2799 ns/op	     881 B/op	      33 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2863 ns/op	     886 B/op	      33 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2801 ns/op	     858 B/op	      32 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2838 ns/op	     878 B/op	      33 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  500000	      2899 ns/op	     889 B/op	      33 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21905878 ns/op	 6583437 B/op	  204039 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21967617 ns/op	 6642746 B/op	  205939 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21583854 ns/op	 6588870 B/op	  204312 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21609599 ns/op	 6633715 B/op	  205637 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	     100	  21720268 ns/op	 6582530 B/op	  204108 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3664 ns/op	    1145 B/op	      39 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3476 ns/op	    1168 B/op	      40 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3487 ns/op	    1150 B/op	      39 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3544 ns/op	    1154 B/op	      40 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  500000	      3412 ns/op	    1166 B/op	      40 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  22923400 ns/op	 6305977 B/op	  202064 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  22732759 ns/op	 6299839 B/op	  201837 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  23036891 ns/op	 6221560 B/op	  199385 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  22350515 ns/op	 6179889 B/op	  198121 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	     100	  23027119 ns/op	 6262824 B/op	  200629 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3353 ns/op	    1029 B/op	      38 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3396 ns/op	    1015 B/op	      37 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3384 ns/op	    1013 B/op	      37 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3454 ns/op	    1016 B/op	      37 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  500000	      3349 ns/op	    1028 B/op	      38 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  66863366 ns/op	17088409 B/op	  520780 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  65763199 ns/op	17214454 B/op	  524796 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  67129051 ns/op	17168499 B/op	  523464 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  66670352 ns/op	17055736 B/op	  519778 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      30	  66434197 ns/op	17216688 B/op	  524492 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1252 ns/op	     233 B/op	      10 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1162 ns/op	     244 B/op	      10 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1315 ns/op	     251 B/op	      11 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1187 ns/op	     240 B/op	      10 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      1323 ns/op	     253 B/op	      11 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  21859272 ns/op	128838031 B/op	  103292 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  22699363 ns/op	128848017 B/op	  103603 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  21826765 ns/op	128784471 B/op	  101618 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  21667325 ns/op	128808870 B/op	  102381 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	     100	  22453480 ns/op	128834411 B/op	  103178 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1538 ns/op	     696 B/op	      16 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1506 ns/op	     702 B/op	      16 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1510 ns/op	     708 B/op	      17 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1520 ns/op	     688 B/op	      16 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1442 ns/op	     694 B/op	      16 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      20	  54704611 ns/op	227761291 B/op	  270608 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  48909419 ns/op	233258953 B/op	  222040 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  47253632 ns/op	233259122 B/op	  221626 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  46212659 ns/op	232961277 B/op	  212274 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  48044707 ns/op	233072465 B/op	  216352 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     15528 ns/op	    6983 B/op	     121 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     16636 ns/op	    7085 B/op	     126 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     14784 ns/op	    6992 B/op	     123 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     17371 ns/op	    7509 B/op	     137 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     20663 ns/op	    7204 B/op	     133 allocs/op
BenchmarkMCacheWithBigDataset-8        	      20	  71301439 ns/op	19404819 B/op	  347395 allocs/op
BenchmarkMCacheWithBigDataset-8        	      30	  58963700 ns/op	19795017 B/op	  390273 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  41901923 ns/op	11794883 B/op	  298868 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  41806838 ns/op	13804077 B/op	  332976 allocs/op
BenchmarkMCacheWithBigDataset-8        	      50	  41032910 ns/op	14261304 B/op	  343917 allocs/op
PASS
ok  	github.com/kpango/gache	210.683s
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
