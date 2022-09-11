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
Benchmark results are shown below and benchmarked in [this](https://github.com/kpango/go-cache-lib-benchmarks) repository

```ltsv
go test -count=1 -run=NONE -bench . -benchmem
goos: linux
goarch: amd64
pkg: github.com/kpango/go-cache-lib-benchmarks
cpu: Intel(R) Core(TM) i9-9880H CPU @ 2.30GHz
BenchmarkDefaultMapSetSmallDataNoTTL-16     	 3378945	       916.9 ns/op	       4 B/op	       0 allocs/op
BenchmarkDefaultMapSetBigDataNoTTL-16       	   12446	    198876 ns/op	    1381 B/op	      27 allocs/op
BenchmarkSyncMapSetSmallDataNoTTL-16        	 5364061	       222.5 ns/op	     194 B/op	      12 allocs/op
BenchmarkSyncMapSetBigDataNoTTL-16          	   16467	     73703 ns/op	   25447 B/op	    1556 allocs/op
BenchmarkGacheSetSmallDataNoTTL-16          	 1874124	       533.7 ns/op	     231 B/op	      12 allocs/op
BenchmarkGacheSetSmallDataWithTTL-16        	 1901086	       558.5 ns/op	     231 B/op	      12 allocs/op
BenchmarkGacheSetBigDataNoTTL-16            	   28442	     38329 ns/op	   29166 B/op	    1547 allocs/op
BenchmarkGacheSetBigDataWithTTL-16          	   30856	     38755 ns/op	   29162 B/op	    1547 allocs/op
BenchmarkTTLCacheSetSmallDataNoTTL-16       	  862920	      1633 ns/op	     208 B/op	       4 allocs/op
BenchmarkTTLCacheSetSmallDataWithTTL-16     	  274393	      4322 ns/op	     242 B/op	       5 allocs/op
BenchmarkTTLCacheSetBigDataNoTTL-16         	    4968	    334135 ns/op	   27287 B/op	     577 allocs/op
BenchmarkTTLCacheSetBigDataWithTTL-16       	    2004	    728444 ns/op	   31149 B/op	     673 allocs/op
BenchmarkGoCacheSetSmallDataNoTTL-16        	 2186665	      1587 ns/op	      70 B/op	       4 allocs/op
BenchmarkGoCacheSetSmallDataWithTTL-16      	 1074262	      2541 ns/op	      77 B/op	       4 allocs/op
BenchmarkGoCacheSetBigDataNoTTL-16          	    5450	    406692 ns/op	   10659 B/op	     571 allocs/op
BenchmarkGoCacheSetBigDataWithTTL-16        	    3468	    518512 ns/op	   12014 B/op	     605 allocs/op
BenchmarkBigCacheSetSmallDataNoTTL-16       	  673636	      1803 ns/op	     286 B/op	       8 allocs/op
BenchmarkBigCacheSetSmallDataWithTTL-16     	  650728	      1767 ns/op	     294 B/op	       8 allocs/op
BenchmarkBigCacheSetBigDataNoTTL-16         	    3694	    355458 ns/op	 1944206 B/op	    1624 allocs/op
BenchmarkBigCacheSetBigDataWithTTL-16       	    5222	    374449 ns/op	 1675416 B/op	    1598 allocs/op
BenchmarkFastCacheSetSmallDataNoTTL-16      	  909879	      1296 ns/op	      55 B/op	       4 allocs/op
BenchmarkFastCacheSetBigDataNoTTL-16        	   11193	    249992 ns/op	  795568 B/op	    2057 allocs/op
BenchmarkFreeCacheSetSmallDataNoTTL-16      	  913647	      1241 ns/op	     137 B/op	       8 allocs/op
BenchmarkFreeCacheSetSmallDataWithTTL-16    	  994545	      1232 ns/op	     136 B/op	       8 allocs/op
BenchmarkFreeCacheSetBigDataNoTTL-16        	   11065	    300772 ns/op	 1062087 B/op	    2589 allocs/op
BenchmarkFreeCacheSetBigDataWithTTL-16      	   11018	    259470 ns/op	 1061975 B/op	    2589 allocs/op
BenchmarkGCacheLRUSetSmallDataNoTTL-16      	  309943	      5940 ns/op	     747 B/op	      24 allocs/op
BenchmarkGCacheLRUSetSmallDataWithTTL-16    	  565794	      2382 ns/op	     312 B/op	      16 allocs/op
BenchmarkGCacheLRUSetBigDataNoTTL-16        	    2870	    695645 ns/op	  100849 B/op	    3185 allocs/op
BenchmarkGCacheLRUSetBigDataWithTTL-16      	    1455	    717338 ns/op	  105173 B/op	    3294 allocs/op
BenchmarkGCacheLFUSetSmallDataNoTTL-16      	  490386	      4533 ns/op	     545 B/op	      20 allocs/op
BenchmarkGCacheLFUSetSmallDataWithTTL-16    	  527766	      2972 ns/op	     314 B/op	      16 allocs/op
BenchmarkGCacheLFUSetBigDataNoTTL-16        	    1478	    689599 ns/op	   80439 B/op	    2783 allocs/op
BenchmarkGCacheLFUSetBigDataWithTTL-16      	    2341	    549463 ns/op	   74897 B/op	    2697 allocs/op
BenchmarkGCacheARCSetSmallDataNoTTL-16      	  321163	      6582 ns/op	     936 B/op	      28 allocs/op
BenchmarkGCacheARCSetSmallDataWithTTL-16    	  453481	      4034 ns/op	     318 B/op	      16 allocs/op
BenchmarkGCacheARCSetBigDataNoTTL-16        	    1428	   1063506 ns/op	  131515 B/op	    3793 allocs/op
BenchmarkGCacheARCSetBigDataWithTTL-16      	     978	   1199343 ns/op	  130454 B/op	    3859 allocs/op
BenchmarkMCacheSetSmallDataNoTTL-16         	  168380	      7084 ns/op	    2431 B/op	      33 allocs/op
BenchmarkMCacheSetSmallDataWithTTL-16       	  133646	      7996 ns/op	    2412 B/op	      35 allocs/op
BenchmarkMCacheSetBigDataNoTTL-16           	    1773	    855008 ns/op	  307511 B/op	    4277 allocs/op
BenchmarkMCacheSetBigDataWithTTL-16         	    1687	    782911 ns/op	  271697 B/op	    4390 allocs/op
BenchmarkBitcaskSetSmallDataNoTTL-16        	   61621	     18411 ns/op	     667 B/op	      29 allocs/op
BenchmarkBitcaskSetBigDataNoTTL-16          	   12972	    215781 ns/op	  787419 B/op	    1560 allocs/op
PASS
ok  	github.com/kpango/go-cache-lib-benchmarks	105.898s
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
gache released under MIT license, refer [LICENSE](https://github.com/kpango/gache/blob/main/LICENSE) file.


[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fkpango%2Fgache.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fkpango%2Fgache?ref=badge_large)
