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
Go 1.18~

## Installation
```shell
go get github.com/kpango/gache/v2
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
BenchmarkDefaultMapSetSmallDataNoTTL-16     	 3220872	     887.4 ns/op	       4 B/op	       0 allocs/op
BenchmarkDefaultMapSetBigDataNoTTL-16       	    1107	   1290329 ns/op	   11662 B/op	     290 allocs/op
BenchmarkSyncMapSetSmallDataNoTTL-16        	 4918752	     228.7 ns/op	     194 B/op	      12 allocs/op
BenchmarkSyncMapSetBigDataNoTTL-16          	    2088	    637001 ns/op	  104660 B/op	    6300 allocs/op
BenchmarkGacheV2SetSmallDataNoTTL-16        	 5839593	     189.0 ns/op	      98 B/op	       4 allocs/op
BenchmarkGacheV2SetSmallDataWithTTL-16      	 6647318	     186.1 ns/op	      98 B/op	       4 allocs/op
BenchmarkGacheV2SetBigDataNoTTL-16          	    8611	    143677 ns/op	   50736 B/op	    2086 allocs/op
BenchmarkGacheV2SetBigDataWithTTL-16        	    8865	    136431 ns/op	   50695 B/op	    2085 allocs/op
BenchmarkGacheSetSmallDataNoTTL-16          	 4785205	     226.0 ns/op	     162 B/op	       8 allocs/op
BenchmarkGacheSetSmallDataWithTTL-16        	 4134656	     256.6 ns/op	     163 B/op	       8 allocs/op
BenchmarkGacheSetBigDataNoTTL-16            	    7540	    160175 ns/op	   83680 B/op	    4139 allocs/op
BenchmarkGacheSetBigDataWithTTL-16          	    7886	    153134 ns/op	   83623 B/op	    4137 allocs/op
BenchmarkTTLCacheSetSmallDataNoTTL-16       	  814436	      1646 ns/op	     209 B/op	       4 allocs/op
BenchmarkTTLCacheSetSmallDataWithTTL-16     	  370612	      5213 ns/op	     229 B/op	       4 allocs/op
BenchmarkTTLCacheSetBigDataNoTTL-16         	     998	   1856747 ns/op	  111887 B/op	    2375 allocs/op
BenchmarkTTLCacheSetBigDataWithTTL-16       	     639	   3848022 ns/op	  119504 B/op	    2559 allocs/op
BenchmarkGoCacheSetSmallDataNoTTL-16        	 2363812	      1755 ns/op	      69 B/op	       4 allocs/op
BenchmarkGoCacheSetSmallDataWithTTL-16      	  689509	      1812 ns/op	      84 B/op	       4 allocs/op
BenchmarkGoCacheSetBigDataNoTTL-16          	     790	   2399635 ns/op	   49599 B/op	    2455 allocs/op
BenchmarkGoCacheSetBigDataWithTTL-16        	     811	   2432933 ns/op	   49173 B/op	    2444 allocs/op
BenchmarkBigCacheSetSmallDataNoTTL-16       	  684067	      2060 ns/op	     282 B/op	       8 allocs/op
BenchmarkBigCacheSetSmallDataWithTTL-16     	  675710	      1815 ns/op	     341 B/op	       8 allocs/op
BenchmarkBigCacheSetBigDataNoTTL-16         	     453	   6379053 ns/op	22970362 B/op	    6839 allocs/op
BenchmarkBigCacheSetBigDataWithTTL-16       	     391	   6044775 ns/op	24995056 B/op	    6975 allocs/op
BenchmarkFastCacheSetSmallDataNoTTL-16      	  914353	      1602 ns/op	      55 B/op	       4 allocs/op
BenchmarkFastCacheSetBigDataNoTTL-16        	     523	   3062973 ns/op	12630167 B/op	    8158 allocs/op
BenchmarkFreeCacheSetSmallDataNoTTL-16      	  944091	      1289 ns/op	     139 B/op	       8 allocs/op
BenchmarkFreeCacheSetSmallDataWithTTL-16    	  931282	      1301 ns/op	     137 B/op	       8 allocs/op
BenchmarkFreeCacheSetBigDataNoTTL-16        	     513	   4004331 ns/op	16851934 B/op	   10866 allocs/op
BenchmarkFreeCacheSetBigDataWithTTL-16      	     523	   4192427 ns/op	16685501 B/op	   10698 allocs/op
BenchmarkGCacheLRUSetSmallDataNoTTL-16      	  426148	      4184 ns/op	     730 B/op	      24 allocs/op
BenchmarkGCacheLRUSetSmallDataWithTTL-16    	  484747	      2965 ns/op	     316 B/op	      16 allocs/op
BenchmarkGCacheLRUSetBigDataNoTTL-16        	     604	   3597852 ns/op	  408744 B/op	   12845 allocs/op
BenchmarkGCacheLRUSetBigDataWithTTL-16      	     594	   3801168 ns/op	  410085 B/op	   12857 allocs/op
BenchmarkGCacheLFUSetSmallDataNoTTL-16      	  437350	      4308 ns/op	     553 B/op	      20 allocs/op
BenchmarkGCacheLFUSetSmallDataWithTTL-16    	  479011	      3197 ns/op	     317 B/op	      16 allocs/op
BenchmarkGCacheLFUSetBigDataNoTTL-16        	     549	   3620129 ns/op	  312066 B/op	   10870 allocs/op
BenchmarkGCacheLFUSetBigDataWithTTL-16      	     542	   3383455 ns/op	  305377 B/op	   10841 allocs/op
BenchmarkGCacheARCSetSmallDataNoTTL-16      	  338374	      7514 ns/op	     930 B/op	      28 allocs/op
BenchmarkGCacheARCSetSmallDataWithTTL-16    	  472202	      3993 ns/op	     317 B/op	      16 allocs/op
BenchmarkGCacheARCSetBigDataNoTTL-16        	     278	   8533520 ns/op	  503447 B/op	   14677 allocs/op
BenchmarkGCacheARCSetBigDataWithTTL-16      	     304	   7159431 ns/op	  482107 B/op	   14383 allocs/op
BenchmarkMCacheSetSmallDataNoTTL-16         	  178670	      7234 ns/op	    2429 B/op	      33 allocs/op
BenchmarkMCacheSetSmallDataWithTTL-16       	  141013	      7485 ns/op	    2152 B/op	      35 allocs/op
BenchmarkMCacheSetBigDataNoTTL-16           	     418	   4344731 ns/op	 1113775 B/op	   17152 allocs/op
BenchmarkMCacheSetBigDataWithTTL-16         	     412	   3336398 ns/op	 1082287 B/op	   17522 allocs/op
BenchmarkBitcaskSetSmallDataNoTTL-16        	   42202	     26318 ns/op	     776 B/op	      31 allocs/op
BenchmarkBitcaskSetBigDataNoTTL-16          	     553	   2336656 ns/op	12606064 B/op	    6722 allocs/op
PASS
ok  	github.com/kpango/go-cache-lib-benchmarks	134.203s
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
