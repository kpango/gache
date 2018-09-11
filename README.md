# gache [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![release](https://img.shields.io/github/release/kpango/gache.svg)](https://github.com/kpango/gache/releases/latest) [![CircleCI](https://circleci.com/gh/kpango/gache.svg?style=shield)](https://circleci.com/gh/kpango/gache) [![codecov](https://codecov.io/gh/kpango/gache/branch/master/graph/badge.svg)](https://codecov.io/gh/kpango/gache) [![Codacy Badge](https://api.codacy.com/project/badge/Grade/ac73fd76d01140a38c5650b9278bc971)](https://www.codacy.com/app/i.can.feel.gravity/gache?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=kpango/gache&amp;utm_campaign=Badge_Grade) [![Go Report Card](https://goreportcard.com/badge/github.com/kpango/gache)](https://goreportcard.com/report/github.com/kpango/gache) [![GoDoc](http://godoc.org/github.com/kpango/gache?status.svg)](http://godoc.org/github.com/kpango/gache) [![Join the chat at https://gitter.im/kpango/gache](https://badges.gitter.im/kpango/gache.svg)](https://gitter.im/kpango/gache?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)

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

[gache](https://github.com/kpango/gache) vs [normal map with lock](https://github.com/kpango/gache/blob/master/gache_bench_test.go#L13-L35) vs [bigcache](https://github.com/allegro/bigcache) vs [go-cache](https://github.com/patrickmn/go-cache) vs [gcache](https://github.com/bluele/gcache) vs [freecache](https://github.com/coocood/freecache) vs [gocache](https://github.com/hlts2/gocache)

![Bench](https://github.com/kpango/gache/raw/master/images/bench.png)

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
