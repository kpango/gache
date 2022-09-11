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
BenchmarkDefaultMapSetSmallDataNoTTL-16     	  459644	      2258 ns/op	      30 B/op	       0 allocs/op
BenchmarkDefaultMapSetBigDataNoTTL-16       	     206	   5765887 ns/op	   62292 B/op	    1557 allocs/op
BenchmarkSyncMapSetSmallDataNoTTL-16        	 2661999	       405.2 ns/op	     197 B/op	      12 allocs/op
BenchmarkSyncMapSetBigDataNoTTL-16          	     326	   3570212 ns/op	  138587 B/op	    7140 allocs/op
BenchmarkGacheSetSmallDataNoTTL-16          	 5906605	       208.9 ns/op	      98 B/op	       4 allocs/op
BenchmarkGacheSetSmallDataWithTTL-16        	 5902875	       204.0 ns/op	      98 B/op	       4 allocs/op
BenchmarkGacheSetBigDataNoTTL-16            	    6529	    169441 ns/op	   51219 B/op	    2098 allocs/op
BenchmarkGacheSetBigDataWithTTL-16          	    7269	    163060 ns/op	   51244 B/op	    2094 allocs/op
BenchmarkTTLCacheSetSmallDataNoTTL-16       	  200223	      6566 ns/op	     261 B/op	       5 allocs/op
BenchmarkTTLCacheSetSmallDataWithTTL-16     	   92175	     13164 ns/op	     343 B/op	       7 allocs/op
BenchmarkTTLCacheSetBigDataNoTTL-16         	     193	   6328993 ns/op	  167956 B/op	    3732 allocs/op
BenchmarkTTLCacheSetBigDataWithTTL-16       	     144	  10233574 ns/op	  191882 B/op	    4308 allocs/op
BenchmarkGoCacheSetSmallDataNoTTL-16        	  389670	      3831 ns/op	      99 B/op	       4 allocs/op
BenchmarkGoCacheSetSmallDataWithTTL-16      	  250716	      4097 ns/op	     119 B/op	       5 allocs/op
BenchmarkGoCacheSetBigDataNoTTL-16          	     141	   7488216 ns/op	  126616 B/op	    4323 allocs/op
BenchmarkGoCacheSetBigDataWithTTL-16        	     139	   8471783 ns/op	  128016 B/op	    4356 allocs/op
BenchmarkBigCacheSetSmallDataNoTTL-16       	  236119	      6956 ns/op	     294 B/op	       9 allocs/op
BenchmarkBigCacheSetSmallDataWithTTL-16     	  389678	      6691 ns/op	     196 B/op	       8 allocs/op
BenchmarkBigCacheSetBigDataNoTTL-16         	     124	   9869875 ns/op	23393128 B/op	    8747 allocs/op
BenchmarkBigCacheSetBigDataWithTTL-16       	     392	   5429973 ns/op	25367032 B/op	    6972 allocs/op
BenchmarkFastCacheSetSmallDataNoTTL-16      	  167629	      6659 ns/op	     123 B/op	       5 allocs/op
BenchmarkFastCacheSetBigDataNoTTL-16        	     417	   2943088 ns/op	12638160 B/op	    8419 allocs/op
BenchmarkFreeCacheSetSmallDataNoTTL-16      	  766485	      2027 ns/op	     140 B/op	       8 allocs/op
BenchmarkFreeCacheSetSmallDataWithTTL-16    	  571834	      3179 ns/op	     146 B/op	       8 allocs/op
BenchmarkFreeCacheSetBigDataNoTTL-16        	     406	   3783413 ns/op	16858522 B/op	   11030 allocs/op
BenchmarkFreeCacheSetBigDataWithTTL-16      	     349	   3540414 ns/op	15614019 B/op	    9958 allocs/op
BenchmarkGCacheLRUSetSmallDataNoTTL-16      	  187882	      9479 ns/op	     770 B/op	      25 allocs/op
BenchmarkGCacheLRUSetSmallDataWithTTL-16    	  165115	      8869 ns/op	     372 B/op	      18 allocs/op
BenchmarkGCacheLRUSetBigDataNoTTL-16        	     121	   9916428 ns/op	  492541 B/op	   14957 allocs/op
BenchmarkGCacheLRUSetBigDataWithTTL-16      	     166	   9096793 ns/op	  464247 B/op	   14240 allocs/op
BenchmarkGCacheLFUSetSmallDataNoTTL-16      	  127224	     12274 ns/op	     626 B/op	      22 allocs/op
BenchmarkGCacheLFUSetSmallDataWithTTL-16    	  121626	      9688 ns/op	     402 B/op	      18 allocs/op
BenchmarkGCacheLFUSetBigDataNoTTL-16        	     105	  11101209 ns/op	  410300 B/op	   13334 allocs/op
BenchmarkGCacheLFUSetBigDataWithTTL-16      	     104	  11035565 ns/op	  409079 B/op	   13353 allocs/op
BenchmarkGCacheARCSetSmallDataNoTTL-16      	   70276	     19598 ns/op	    1077 B/op	      31 allocs/op
BenchmarkGCacheARCSetSmallDataWithTTL-16    	  115950	     10583 ns/op	     408 B/op	      18 allocs/op
BenchmarkGCacheARCSetBigDataNoTTL-16        	      64	  20402268 ns/op	  617883 B/op	   17682 allocs/op
BenchmarkGCacheARCSetBigDataWithTTL-16      	      80	  19397749 ns/op	  580357 B/op	   16840 allocs/op
BenchmarkMCacheSetSmallDataNoTTL-16         	   41936	     31183 ns/op	    2645 B/op	      39 allocs/op
BenchmarkMCacheSetSmallDataWithTTL-16       	   64762	     30425 ns/op	    2561 B/op	      37 allocs/op
BenchmarkMCacheSetBigDataNoTTL-16           	      82	  13488750 ns/op	 1303606 B/op	   20296 allocs/op
BenchmarkMCacheSetBigDataWithTTL-16         	      82	  13824173 ns/op	 1215744 B/op	   20707 allocs/op
BenchmarkBitcaskSetSmallDataNoTTL-16        	   20850	     55945 ns/op	    1149 B/op	      40 allocs/op
BenchmarkBitcaskSetBigDataNoTTL-16          	     500	   4030866 ns/op	12608528 B/op	    6784 allocs/op
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
