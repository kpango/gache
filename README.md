<div align="center">
<img src="./assets/logo.png" width="50%">
</div>


[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![release](https://img.shields.io/github/release/kpango/gache.svg)](https://github.com/kpango/gache/releases/latest)
[![CircleCI](https://circleci.com/gh/kpango/gache.svg?style=shield)](https://circleci.com/gh/kpango/gache)
[![Codacy Badge](https://api.codacy.com/project/badge/Grade/ac73fd76d01140a38c5650b9278bc971)](https://www.codacy.com/app/i.can.feel.gravity/gache?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=kpango/gache&amp;utm_campaign=Badge_Grade)
[![Go Report Card](https://goreportcard.com/badge/github.com/kpango/gache)](https://goreportcard.com/report/github.com/kpango/gache)
[![GoDoc](http://godoc.org/github.com/kpango/gache?status.svg)](http://godoc.org/github.com/kpango/gache)
[![Join the chat at https://gitter.im/kpango/gache](https://badges.gitter.im/kpango/gache.svg)](https://gitter.im/kpango/gache?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)
[![DepShield Badge](https://depshield.sonatype.org/badges/kpango/gache/depshield.svg)](https://depshield.github.io)
[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fkpango%2Fgache.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Fkpango%2Fgache?ref=badge_shield)
[![Total visitor](https://visitor-count-badge.herokuapp.com/total.svg?repo_id=gache)](https://github.com/kpango/gache/graphs/traffic)
[![Visitors in today](https://visitor-count-badge.herokuapp.com/today.svg?repo_id=gache)](https://github.com/kpango/gache/graphs/traffic)

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

        // instantiate gache for any type as gc with setup default expiration.
        // see more Options in example/main.go
	gc := gache.New[any]().SetDefaultExpire(time.Second * 10)

	// store with expire setting
	gc.SetWithExpire(key1, value1, time.Second*30)
	gc.SetWithExpire(key2, value2, time.Second*60)
	gc.SetWithExpire(key3, value3, time.Hour)	// load cache data
	v1, ok := gc.Get(key1)

	v2, ok := gc.Get(key2)

	v3, ok := gc.Get(key3)

        // open exported cache file
        file, err := os.OpenFile("./gache-sample.gdb", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		glg.Error(err)
		return
	}

        // export cached variable with expiration time 
	gc.Write(context.Background(), file)
        file.Close()

        // open exported cache file
	file, err = os.OpenFile("./gache-sample.gdb", os.O_RDONLY, 0755)
	if err != nil {
		glg.Error(err)
		return
	}
        defer file.Close()

        // instantiate new gache for any type as gcn with load exported cache from file
	gcn := gache.New[any]().SetDefaultExpire(time.Minute).Read(file)

        // gache supports range loop processing method
	gcn.Range(context.Background(), func(k string, v any, exp int64) bool {
		glg.Debugf("key:\t%v\nval:\t%v", k, v)
		return true
	})

        // instantiate new gache for int64 type as gci
        gci := gache.New[int64]()

        gci.Set("sample1", int64(0))
        gci.Set("sample2", int64(10))
        gci.Set("sample3", int64(100))

        // gache supports range loop processing method and inner function argument is int64 as contract
	gci.Range(context.Background(), func(k string, v int64, exp int64) bool {
		glg.Debugf("key:\t%v\nval:\t%d", k, v)
		return true
	})

```
## Benchmarks

[gache](https://github.com/kpango/gache) vs [gocache](https://github.com/hlts2/gocache) vs [normal map with lock](https://github.com/kpango/gache/blob/master/gache_bench_test.go#L13-L35) vs [go-cache](https://github.com/patrickmn/go-cache) vs [gcache](https://github.com/bluele/gcache) vs [freecache](https://github.com/coocood/freecache) vs [bigcache](https://github.com/allegro/bigcache) vs [go-mcache](https://github.com/OrlovEvgeny/go-mcache)


```ltsv
go test -count=1 -run=NONE -bench . -benchmem

goos: darwin
goarch: amd64
pkg: github.com/kpango/gache
BenchmarkGacheWithSmallDataset-8       	 5000000	       250 ns/op	     192 B/op	       8 allocs/op
BenchmarkGacheWithBigDataset-8         	     500	   3179552 ns/op	  485156 B/op	   20160 allocs/op
BenchmarkGocacheWithSmallDataset-8     	 3000000	       378 ns/op	     323 B/op	      16 allocs/op
BenchmarkGocacheWithBigDataset-8       	     300	   3564275 ns/op	  815303 B/op	   40352 allocs/op
BenchmarkFastCacheWithSmallDataset-8   	 1000000	      1496 ns/op	      44 B/op	       4 allocs/op
BenchmarkFastCacheWithBigDataset-8     	      50	  36815105 ns/op	126848505 B/op	   41603 allocs/op
BenchmarkBigCacheWithSmallDataset-8    	 1000000	      1915 ns/op	     424 B/op	       8 allocs/op
BenchmarkBigCacheWithBigDataset-8      	      30	  62743673 ns/op	227737772 B/op	   32892 allocs/op
BenchmarkFreeCacheWithSmallDataset-8   	 1000000	      2659 ns/op	      31 B/op	       4 allocs/op
BenchmarkFreeCacheWithBigDataset-8     	      50	  26550884 ns/op	126889120 B/op	   41552 allocs/op
BenchmarkMapWithSmallDataset-8         	  500000	      4221 ns/op	     137 B/op	       8 allocs/op
BenchmarkMapWithBigDataset-8           	     100	  10926857 ns/op	  358593 B/op	   20808 allocs/op
BenchmarkGoCacheWithSmallDataset-8     	  500000	      3870 ns/op	      73 B/op	       4 allocs/op
BenchmarkGoCacheWithBigDataset-8       	     100	  10858366 ns/op	  201482 B/op	   10809 allocs/op
BenchmarkGCacheLRUWithSmallDataset-8   	  200000	      7913 ns/op	     348 B/op	      16 allocs/op
BenchmarkGCacheLRUWithBigDataset-8     	      50	  21157440 ns/op	 2026806 B/op	   61756 allocs/op
BenchmarkGCacheLFUWithSmallDataset-8   	  200000	     10704 ns/op	     542 B/op	      20 allocs/op
BenchmarkGCacheLFUWithBigDataset-8     	      50	  22017917 ns/op	 1491388 B/op	   51602 allocs/op
BenchmarkGCacheARCWithSmallDataset-8   	  200000	     10987 ns/op	     350 B/op	      16 allocs/op
BenchmarkGCacheARCWithBigDataset-8     	      20	  64480186 ns/op	 3068799 B/op	   84008 allocs/op
BenchmarkMCacheWithSmallDataset-8      	  100000	     18578 ns/op	    4373 B/op	      41 allocs/op
BenchmarkMCacheWithBigDataset-8        	      30	  34827645 ns/op	10851422 B/op	  102187 allocs/op
BenchmarkBitcaskWithSmallDataset-8     	   30000	     55917 ns/op	    2455 B/op	      49 allocs/op
BenchmarkBitcaskWithBigDataset-8       	     200	  10602661 ns/op	10502332 B/op	   11091 allocs/op
PASS
ok  	github.com/kpango/gache	67.249s
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
