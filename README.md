<div align="center">
<img src="./assets/logo.png" width="50%">
</div>


[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![release](https://img.shields.io/github/release/kpango/gache.svg)](https://github.com/kpango/gache/releases/latest)
[![CircleCI](https://circleci.com/gh/kpango/gache.svg?style=shield)](https://circleci.com/gh/kpango/gache)
[![Codacy Badge](https://api.codacy.com/project/badge/Grade/ac73fd76d01140a38c5650b9278bc971)](https://www.codacy.com/app/i.can.feel.gravity/gache?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=kpango/gache&amp;utm_campaign=Badge_Grade)
[![Go Report Card](https://goreportcard.com/badge/github.com/kpango/gache)](https://goreportcard.com/report/github.com/kpango/gache)
[![Go Reference](https://pkg.go.dev/badge/github.com/kpango/gache/v2.svg)](https://pkg.go.dev/github.com/kpango/gache/v2)
[![Join the chat at https://gitter.im/kpango/gache](https://badges.gitter.im/kpango/gache.svg)](https://gitter.im/kpango/gache?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)
[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fkpango%2Fgache.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Fkpango%2Fgache?ref=badge_shield)

gache is the thinnest cache library for Go applications.

It provides a **generic, type-safe, concurrent-safe in-memory cache** with TTL (time-to-live) support. gache uses a sharded architecture (4096 shards) to minimize lock contention, making it ideal for high-throughput, concurrent workloads. It outperforms `sync.Map`, [go-cache](https://github.com/patrickmn/go-cache), [bigcache](https://github.com/allegro/bigcache), [gcache](https://github.com/bluele/gcache), and [ttlcache](https://github.com/jellydator/ttlcache) in benchmarks while providing a richer feature set.

## Features

- **Go Generics** – Full type safety via `Gache[V any]`; no type assertions required.
- **High Performance** – Sharded storage with 4096 shards and lock-free reads minimize contention.
- **TTL / Expiration** – Per-key and default TTL support. Use `gache.NoTTL` for entries that should never expire.
- **Background Expiration** – Optional daemon (`StartExpired`) periodically removes expired entries.
- **Expiration Hooks** – Register a callback that fires when entries expire.
- **Serialization** – Export/import the cache to/from any `io.Writer`/`io.Reader` using gob encoding.
- **Concurrent-Safe** – All operations are safe for use by multiple goroutines.
- **Zero Dependencies for Core** – Only lightweight, well-maintained dependencies ([fastime](https://github.com/kpango/fastime), [xxh3](https://github.com/zeebo/xxh3)).

## Requirement

Go 1.18 or later (generics support is required).

## Installation

```shell
go get github.com/kpango/gache/v2
```

## Quick Start

### Basic Set / Get

```go
package main

import (
	"fmt"

	"github.com/kpango/gache/v2"
)

func main() {
	// Create a new cache for string values with default settings.
	gc := gache.New[string]()

	// Store a value.
	gc.Set("greeting", "hello")

	// Retrieve the value.
	if v, ok := gc.Get("greeting"); ok {
		fmt.Println(v) // "hello"
	}
}
```

### Set / Get with TTL

```go
package main

import (
	"fmt"
	"time"

	"github.com/kpango/gache/v2"
)

func main() {
	// Create a cache with a 10-second default TTL.
	gc := gache.New[string]().SetDefaultExpire(time.Second * 10)

	// Store with a custom TTL (overrides the default).
	gc.SetWithExpire("session", "abc123", time.Minute*30)

	// Retrieve the value.
	if v, ok := gc.Get("session"); ok {
		fmt.Println(v) // "abc123"
	}

	// Store an entry that never expires.
	gc.SetWithExpire("permanent", "forever", gache.NoTTL)
}
```

### Background Expiration and Hooks

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kpango/gache/v2"
)

func main() {
	gc := gache.New[string](
		gache.WithDefaultExpiration[string](time.Second * 5),
	).SetExpiredHook(func(ctx context.Context, key string, val string) {
		fmt.Printf("expired: key=%s val=%s\n", key, val)
	}).EnableExpiredHook().
		StartExpired(context.Background(), time.Second*2)

	gc.Set("temp", "data")

	// After ~5 seconds the hook prints: expired: key=temp val=data
	time.Sleep(time.Second * 8)

	// Stop the background expiration daemon when done.
	gc.Stop()
}
```

### Type-Safe Caches

```go
// int64 cache – the compiler enforces the value type.
gci := gache.New[int64]()
gci.Set("counter", int64(42))
if v, ok := gci.Get("counter"); ok {
	fmt.Println(v + 1) // 43
}

// struct cache
type User struct {
	Name string
	Age  int
}
gcu := gache.New[User]()
gcu.Set("user:1", User{Name: "Alice", Age: 30})
```

## Example

A full working example is available in [`example/main.go`](./example/main.go). It demonstrates:

- Creating caches for `any`, `int64`, and `string` types.
- Storing and retrieving values with expiration.
- Exporting and importing cache data to/from a file.
- Iterating over entries with `Range`.
- Using expiration hooks and background expiration.
- Stress-testing with large datasets.

```go
// data sets
var (
	key1   = "key1"
	key2   = "key2"
	key3   = "key3"
	value1 = "value"
	value2 = 88888
	value3 = struct{}{}
)

// Create a cache for any type with a 10-second default TTL.
gc := gache.New[any]().SetDefaultExpire(time.Second * 10)

// Store entries with per-key TTLs.
gc.SetWithExpire(key1, value1, time.Second*30)
gc.SetWithExpire(key2, value2, time.Second*60)
gc.SetWithExpire(key3, value3, time.Hour)

// Retrieve entries.
v1, ok := gc.Get(key1)
v2, ok := gc.Get(key2)
v3, ok := gc.Get(key3)

// Export the cache to a file.
file, err := os.OpenFile("./gache-sample.gdb", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0755)
if err != nil {
	log.Fatal(err)
}
gc.Write(context.Background(), file)
file.Close()

// Import the cache from the file into a new instance.
file, err = os.OpenFile("./gache-sample.gdb", os.O_RDONLY, 0755)
if err != nil {
	log.Fatal(err)
}
defer file.Close()
gcn := gache.New[any]().SetDefaultExpire(time.Minute)
gcn.Read(file)

// Iterate over all entries.
gcn.Range(context.Background(), func(k string, v any, exp int64) bool {
	fmt.Printf("key: %v, val: %v\n", k, v)
	return true
})
```

## API Overview

Below is a summary of the `Gache[V any]` interface. For full documentation see the [Go Reference](https://pkg.go.dev/github.com/kpango/gache/v2).

### Core Operations

| Method | Description |
|--------|-------------|
| `New[V any](opts ...Option[V]) Gache[V]` | Create a new cache instance (default TTL: 30s). |
| `Set(key string, val V)` | Store a value with the default TTL. |
| `SetWithExpire(key string, val V, dur time.Duration)` | Store a value with a custom TTL. |
| `Get(key string) (V, bool)` | Retrieve a value. Returns `false` if not found or expired. |
| `Delete(key string) (V, bool)` | Remove an entry and return its value. |
| `Clear()` | Remove all entries from the cache. |

### Advanced Get / Set

| Method | Description |
|--------|-------------|
| `GetWithExpire(key string) (V, int64, bool)` | Get a value together with its expiration time (Unix nano). |
| `GetRefresh(key string) (V, bool)` | Get a value and refresh its TTL to the default duration. |
| `GetRefreshWithDur(key string, dur time.Duration) (V, bool)` | Get a value and set a new TTL. |
| `GetWithIgnoredExpire(key string) (V, bool)` | Get a value even if it has expired. |
| `Pop(key string) (V, bool)` | Get a value and remove it from the cache in one step. |
| `SetIfNotExists(key string, val V)` | Store only if the key does not already exist. |
| `SetWithExpireIfNotExists(key string, val V, dur time.Duration)` | Conditional set with a custom TTL. |
| `ExtendExpire(key string, dur time.Duration)` | Extend the TTL of an existing entry. |

### Expiration Management

| Method | Description |
|--------|-------------|
| `SetDefaultExpire(dur time.Duration) Gache[V]` | Change the default TTL. |
| `StartExpired(ctx context.Context, dur time.Duration) Gache[V]` | Start a background daemon that removes expired entries at the given interval. |
| `DeleteExpired(ctx context.Context) uint64` | Manually remove all expired entries; returns the number removed. |
| `Stop()` | Stop the background expiration daemon. |
| `SetExpiredHook(f func(context.Context, string, V)) Gache[V]` | Register a function called when an entry expires. |
| `EnableExpiredHook() Gache[V]` | Enable the expiration hook. |
| `DisableExpiredHook() Gache[V]` | Disable the expiration hook. |

### Iteration and Inspection

| Method | Description |
|--------|-------------|
| `Range(ctx context.Context, f func(string, V, int64) bool) Gache[V]` | Iterate over all entries. Return `false` from `f` to stop early. |
| `Keys(ctx context.Context) []string` | Return all keys currently in the cache. |
| `Values(ctx context.Context) []V` | Return all values currently in the cache. |
| `Len() int` | Return the number of entries (including expired but not yet cleaned). |
| `Size() uintptr` | Return the approximate memory usage in bytes. |

### Serialization

| Method | Description |
|--------|-------------|
| `Write(ctx context.Context, w io.Writer) error` | Export the cache contents to a writer using gob encoding. |
| `Read(r io.Reader) error` | Import cache contents from a reader. |
| `ToMap(ctx context.Context) *sync.Map` | Convert the cache to a `*sync.Map`. |
| `ToRawMap(ctx context.Context) map[string]V` | Convert the cache to a plain Go map. |

### Constructor Options

| Option | Description |
|--------|-------------|
| `WithDefaultExpiration[V](dur time.Duration)` | Set the default TTL for the cache. |
| `WithDefaultExpirationString[V](s string)` | Set the default TTL from a duration string (e.g. `"5m"`). |
| `WithMaxKeyLength[V](n uint64)` | Limit the number of key bytes used for shard selection (default: 256). |
| `WithExpiredHookFunc[V](f func(ctx, key, val))` | Register an expiration hook at construction time. |

## Benchmarks
Benchmark results are shown below and benchmarked in [this](https://github.com/kpango/go-cache-lib-benchmarks) repository

```ltsv
go test -count=1 -timeout=30m -run=NONE -bench . -benchmem
goos: linux
goarch: amd64
pkg: github.com/kpango/go-cache-lib-benchmarks
cpu: AMD Ryzen Threadripper 3990X 64-Core Processor 
BenchmarkDefaultMapSetGetSmallDataNoTTL/P100-128    	 1325295	       820.7 ns/op	     130 B/op	       8 allocs/op
BenchmarkDefaultMapSetGetSmallDataNoTTL/P1000-128   	 1861357	      1437 ns/op	     134 B/op	       8 allocs/op
BenchmarkDefaultMapSetGetSmallDataNoTTL/P10000-128  	 1264531	      1214 ns/op	     209 B/op	      10 allocs/op
BenchmarkDefaultMapSetGetBigDataNoTTL/P100-128      	       8	 187055869 ns/op	 4324350 B/op	  265392 allocs/op
BenchmarkDefaultMapSetGetBigDataNoTTL/P1000-128     	       8	 181473875 ns/op	 5475553 B/op	  294184 allocs/op
BenchmarkDefaultMapSetGetBigDataNoTTL/P10000-128    	       5	 220690015 ns/op	24675384 B/op	  774189 allocs/op
BenchmarkSyncMapSetGetSmallDataNoTTL/P100-128       	  468852	      2421 ns/op	     324 B/op	      12 allocs/op
BenchmarkSyncMapSetGetSmallDataNoTTL/P1000-128      	  646905	      2153 ns/op	     337 B/op	      12 allocs/op
BenchmarkSyncMapSetGetSmallDataNoTTL/P10000-128     	  134409	      7484 ns/op	    1088 B/op	      31 allocs/op
BenchmarkSyncMapSetGetBigDataNoTTL/P100-128         	     150	  15476148 ns/op	10492638 B/op	  393388 allocs/op
BenchmarkSyncMapSetGetBigDataNoTTL/P1000-128        	      87	  13345667 ns/op	10603526 B/op	  396161 allocs/op
BenchmarkSyncMapSetGetBigDataNoTTL/P10000-128       	      64	  20342732 ns/op	12085859 B/op	  433220 allocs/op
BenchmarkGacheV2SetGetSmallDataNoTTL/P100-128       	 4071116	       268.8 ns/op	       0 B/op	       0 allocs/op
BenchmarkGacheV2SetGetSmallDataNoTTL/P1000-128      	 4239015	       263.3 ns/op	       4 B/op	       0 allocs/op
BenchmarkGacheV2SetGetSmallDataNoTTL/P10000-128     	  829630	      1239 ns/op	     123 B/op	       3 allocs/op
BenchmarkGacheV2SetGetSmallDataWithTTL/P100-128     	 4241361	       261.0 ns/op	       0 B/op	       0 allocs/op
BenchmarkGacheV2SetGetSmallDataWithTTL/P1000-128    	 4414032	       265.2 ns/op	       2 B/op	       0 allocs/op
BenchmarkGacheV2SetGetSmallDataWithTTL/P10000-128   	 1549258	       828.6 ns/op	      66 B/op	       1 allocs/op
BenchmarkGacheV2SetGetBigDataNoTTL/P100-128         	     175	  13330066 ns/op	    6118 B/op	     149 allocs/op
BenchmarkGacheV2SetGetBigDataNoTTL/P1000-128        	     288	  15665110 ns/op	   35775 B/op	     891 allocs/op
BenchmarkGacheV2SetGetBigDataNoTTL/P10000-128       	     111	  11865924 ns/op	  922887 B/op	   23067 allocs/op
BenchmarkGacheV2SetGetBigDataWithTTL/P100-128       	     202	  10517910 ns/op	   71942 B/op	     926 allocs/op
BenchmarkGacheV2SetGetBigDataWithTTL/P1000-128      	     178	   7074201 ns/op	  130711 B/op	    2338 allocs/op
BenchmarkGacheV2SetGetBigDataWithTTL/P10000-128     	     121	   8614236 ns/op	  935641 B/op	   22406 allocs/op
BenchmarkGacheSetGetSmallDataNoTTL/P100-128         	 4186824	       295.3 ns/op	     160 B/op	       8 allocs/op
BenchmarkGacheSetGetSmallDataNoTTL/P1000-128        	 3938966	       292.6 ns/op	     162 B/op	       8 allocs/op
BenchmarkGacheSetGetSmallDataNoTTL/P10000-128       	 1315594	       828.4 ns/op	     237 B/op	       9 allocs/op
BenchmarkGacheSetGetSmallDataWithTTL/P100-128       	 4069194	       290.2 ns/op	     160 B/op	       8 allocs/op
BenchmarkGacheSetGetSmallDataWithTTL/P1000-128      	 3971908	       290.7 ns/op	     162 B/op	       8 allocs/op
BenchmarkGacheSetGetSmallDataWithTTL/P10000-128     	 1327264	       808.4 ns/op	     239 B/op	       9 allocs/op
BenchmarkGacheSetGetBigDataNoTTL/P100-128           	     111	  10852804 ns/op	 5252157 B/op	  262376 allocs/op
BenchmarkGacheSetGetBigDataNoTTL/P1000-128          	     126	  12960028 ns/op	 5324244 B/op	  264177 allocs/op
BenchmarkGacheSetGetBigDataNoTTL/P10000-128         	      30	  33683216 ns/op	 8656380 B/op	  347484 allocs/op
BenchmarkGacheSetGetBigDataWithTTL/P100-128         	      43	  23279869 ns/op	 5732087 B/op	  266737 allocs/op
BenchmarkGacheSetGetBigDataWithTTL/P1000-128        	      66	  15942434 ns/op	 5697604 B/op	  268565 allocs/op
BenchmarkGacheSetGetBigDataWithTTL/P10000-128       	      44	  23237133 ns/op	 7893435 B/op	  323972 allocs/op
BenchmarkTTLCacheSetGetSmallDataNoTTL/P100-128      	  882207	      1298 ns/op	       2 B/op	       0 allocs/op
BenchmarkTTLCacheSetGetSmallDataNoTTL/P1000-128     	  845092	      1543 ns/op	      13 B/op	       0 allocs/op
BenchmarkTTLCacheSetGetSmallDataNoTTL/P10000-128    	    3519	    285093 ns/op	   29142 B/op	     727 allocs/op
BenchmarkTTLCacheSetGetSmallDataWithTTL/P100-128    	  387933	      3810 ns/op	       6 B/op	       0 allocs/op
BenchmarkTTLCacheSetGetSmallDataWithTTL/P1000-128   	  390312	      4000 ns/op	      29 B/op	       0 allocs/op
BenchmarkTTLCacheSetGetSmallDataWithTTL/P10000-128  	   29668	     33910 ns/op	    3506 B/op	      86 allocs/op
BenchmarkTTLCacheSetGetBigDataNoTTL/P100-128        	       5	 204745341 ns/op	  207612 B/op	    5177 allocs/op
BenchmarkTTLCacheSetGetBigDataNoTTL/P1000-128       	       5	 224919335 ns/op	 2050129 B/op	   51255 allocs/op
BenchmarkTTLCacheSetGetBigDataNoTTL/P10000-128      	       5	 218444703 ns/op	20483260 B/op	  512064 allocs/op
BenchmarkTTLCacheSetGetBigDataWithTTL/P100-128      	       4	 287848085 ns/op	  259426 B/op	    6476 allocs/op
BenchmarkTTLCacheSetGetBigDataWithTTL/P1000-128     	       4	 302964096 ns/op	 2561812 B/op	   64064 allocs/op
BenchmarkTTLCacheSetGetBigDataWithTTL/P10000-128    	       4	 309252153 ns/op	25602738 B/op	  640073 allocs/op
BenchmarkGoCacheSetGetSmallDataNoTTL/P100-128       	 1929801	       633.2 ns/op	      65 B/op	       4 allocs/op
BenchmarkGoCacheSetGetSmallDataNoTTL/P1000-128      	 2484928	       789.5 ns/op	      68 B/op	       4 allocs/op
BenchmarkGoCacheSetGetSmallDataNoTTL/P10000-128     	 1936200	       630.7 ns/op	     117 B/op	       5 allocs/op
BenchmarkGoCacheSetGetSmallDataWithTTL/P100-128     	 1224057	      1691 ns/op	      65 B/op	       4 allocs/op
BenchmarkGoCacheSetGetSmallDataWithTTL/P1000-128    	  950708	      1344 ns/op	      76 B/op	       4 allocs/op
BenchmarkGoCacheSetGetSmallDataWithTTL/P10000-128   	  855729	      1377 ns/op	     184 B/op	       7 allocs/op
BenchmarkGoCacheSetGetBigDataNoTTL/P100-128         	       6	 215690920 ns/op	 2269766 B/op	  135390 allocs/op
BenchmarkGoCacheSetGetBigDataNoTTL/P1000-128        	       6	 230979864 ns/op	 3805484 B/op	  173790 allocs/op
BenchmarkGoCacheSetGetBigDataNoTTL/P10000-128       	       7	 229134180 ns/op	16728165 B/op	  496844 allocs/op
BenchmarkGoCacheSetGetBigDataWithTTL/P100-128       	       6	 208563272 ns/op	 2268909 B/op	  135381 allocs/op
BenchmarkGoCacheSetGetBigDataWithTTL/P1000-128      	       5	 204595908 ns/op	 4146240 B/op	  182314 allocs/op
BenchmarkGoCacheSetGetBigDataWithTTL/P10000-128     	       4	 258577174 ns/op	27698782 B/op	  771128 allocs/op
BenchmarkBigCacheSetGetSmallDataNoTTL/P100-128      	  270231	      4398 ns/op	     387 B/op	       8 allocs/op
BenchmarkBigCacheSetGetSmallDataNoTTL/P1000-128     	  223383	      4722 ns/op	     351 B/op	       9 allocs/op
BenchmarkBigCacheSetGetSmallDataNoTTL/P10000-128    	  242823	      4873 ns/op	    1399 B/op	      18 allocs/op
BenchmarkBigCacheSetGetSmallDataWithTTL/P100-128    	  262276	      4270 ns/op	     398 B/op	       8 allocs/op
BenchmarkBigCacheSetGetSmallDataWithTTL/P1000-128   	  295706	      4142 ns/op	     276 B/op	       8 allocs/op
BenchmarkBigCacheSetGetSmallDataWithTTL/P10000-128  	  176874	      6012 ns/op	     633 B/op	      22 allocs/op
BenchmarkBigCacheSetGetBigDataNoTTL/P100-128        	       1	1185592807 ns/op	1271035544 B/op	  290139 allocs/op
BenchmarkBigCacheSetGetBigDataNoTTL/P1000-128       	       1	1709417994 ns/op	1870864472 B/op	  519513 allocs/op
BenchmarkBigCacheSetGetBigDataNoTTL/P10000-128      	       1	1633921555 ns/op	2790072184 B/op	 2823394 allocs/op
BenchmarkBigCacheSetGetBigDataWithTTL/P100-128      	       1	1315226054 ns/op	1271036280 B/op	  290166 allocs/op
BenchmarkBigCacheSetGetBigDataWithTTL/P1000-128     	       1	1464488357 ns/op	1558084768 B/op	  519305 allocs/op
BenchmarkBigCacheSetGetBigDataWithTTL/P10000-128    	       1	1191370621 ns/op	703706888 B/op	 2822443 allocs/op
BenchmarkGCacheLRUSetGetSmallDataNoTTL/P100-128     	  343375	      3183 ns/op	     717 B/op	      23 allocs/op
BenchmarkGCacheLRUSetGetSmallDataNoTTL/P1000-128    	  377926	      3641 ns/op	     733 B/op	      24 allocs/op
BenchmarkGCacheLRUSetGetSmallDataNoTTL/P10000-128   	  332992	      4056 ns/op	    1014 B/op	      31 allocs/op
BenchmarkGCacheLRUSetGetSmallDataWithTTL/P100-128   	  458796	      3683 ns/op	     293 B/op	      16 allocs/op
BenchmarkGCacheLRUSetGetSmallDataWithTTL/P1000-128  	  390602	      3041 ns/op	     317 B/op	      16 allocs/op
BenchmarkGCacheLRUSetGetSmallDataWithTTL/P10000-128 	  303390	      3536 ns/op	     628 B/op	      24 allocs/op
BenchmarkGCacheLRUSetGetBigDataNoTTL/P100-128       	       5	 226899462 ns/op	12579105 B/op	  581883 allocs/op
BenchmarkGCacheLRUSetGetBigDataNoTTL/P1000-128      	       5	 250336714 ns/op	14422416 B/op	  627967 allocs/op
BenchmarkGCacheLRUSetGetBigDataNoTTL/P10000-128     	       5	 243371795 ns/op	32855524 B/op	 1088776 allocs/op
BenchmarkGCacheLRUSetGetBigDataWithTTL/P100-128     	       5	 232806613 ns/op	12579201 B/op	  581884 allocs/op
BenchmarkGCacheLRUSetGetBigDataWithTTL/P1000-128    	       5	 232518008 ns/op	14422872 B/op	  627969 allocs/op
BenchmarkGCacheLRUSetGetBigDataWithTTL/P10000-128   	       5	 224906140 ns/op	32854968 B/op	 1088768 allocs/op
BenchmarkGCacheLFUSetGetSmallDataNoTTL/P100-128     	  400503	      3708 ns/op	     530 B/op	      19 allocs/op
BenchmarkGCacheLFUSetGetSmallDataNoTTL/P1000-128    	  388532	      3537 ns/op	     554 B/op	      20 allocs/op
BenchmarkGCacheLFUSetGetSmallDataNoTTL/P10000-128   	  353187	      3377 ns/op	     821 B/op	      27 allocs/op
BenchmarkGCacheLFUSetGetSmallDataWithTTL/P100-128   	  377215	      2732 ns/op	     294 B/op	      16 allocs/op
BenchmarkGCacheLFUSetGetSmallDataWithTTL/P1000-128  	  475148	      3714 ns/op	     312 B/op	      16 allocs/op
BenchmarkGCacheLFUSetGetSmallDataWithTTL/P10000-128 	  419940	      3309 ns/op	     533 B/op	      22 allocs/op
BenchmarkGCacheLFUSetGetBigDataNoTTL/P100-128       	       5	 235838146 ns/op	11321305 B/op	  555674 allocs/op
BenchmarkGCacheLFUSetGetBigDataNoTTL/P1000-128      	       5	 235307749 ns/op	13164232 B/op	  601751 allocs/op
BenchmarkGCacheLFUSetGetBigDataNoTTL/P10000-128     	       5	 218986777 ns/op	31596737 B/op	 1062553 allocs/op
BenchmarkGCacheLFUSetGetBigDataWithTTL/P100-128     	       4	 251297641 ns/op	11792154 B/op	  563512 allocs/op
BenchmarkGCacheLFUSetGetBigDataWithTTL/P1000-128    	       5	 235102039 ns/op	13164120 B/op	  601750 allocs/op
BenchmarkGCacheLFUSetGetBigDataWithTTL/P10000-128   	       5	 228952630 ns/op	31596086 B/op	 1062549 allocs/op
BenchmarkGCacheARCSetGetSmallDataNoTTL/P100-128     	  272907	      5860 ns/op	     905 B/op	      27 allocs/op
BenchmarkGCacheARCSetGetSmallDataNoTTL/P1000-128    	  278677	      6286 ns/op	     936 B/op	      28 allocs/op
BenchmarkGCacheARCSetGetSmallDataNoTTL/P10000-128   	  211556	      6138 ns/op	    1386 B/op	      39 allocs/op
BenchmarkGCacheARCSetGetSmallDataWithTTL/P100-128   	  293352	      4129 ns/op	     296 B/op	      16 allocs/op
BenchmarkGCacheARCSetGetSmallDataWithTTL/P1000-128  	  454358	      3600 ns/op	     313 B/op	      16 allocs/op
BenchmarkGCacheARCSetGetSmallDataWithTTL/P10000-128 	  237105	      4603 ns/op	     723 B/op	      26 allocs/op
BenchmarkGCacheARCSetGetBigDataNoTTL/P100-128       	       2	 577636447 ns/op	34417548 B/op	  734857 allocs/op
BenchmarkGCacheARCSetGetBigDataNoTTL/P1000-128      	       3	 381003655 ns/op	19842962 B/op	  740771 allocs/op
BenchmarkGCacheARCSetGetBigDataNoTTL/P10000-128     	       3	 358527355 ns/op	50563448 B/op	 1508772 allocs/op
BenchmarkGCacheARCSetGetBigDataWithTTL/P100-128     	       2	 597467738 ns/op	34417644 B/op	  734861 allocs/op
BenchmarkGCacheARCSetGetBigDataWithTTL/P1000-128    	       3	 389213407 ns/op	19843562 B/op	  740778 allocs/op
BenchmarkGCacheARCSetGetBigDataWithTTL/P10000-128   	       3	 384577490 ns/op	50562904 B/op	 1508771 allocs/op
PASS
ok  	github.com/kpango/go-cache-lib-benchmarks	2326.540s
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

The Go Gopher character is licensed under the Creative Commons 4.0 Attribution license. The image was originally created by Renee French.

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fkpango%2Fgache.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fkpango%2Fgache?ref=badge_large)
