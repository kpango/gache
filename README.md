# gache
gache is thinnest cache library for go application

## Requirement
Go 1.9

## Installation
```shell
go get github.com/kpango/gache
```

## Example
### Common Cache Example
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
### Server-Side Cache Example
```go
func handler(w http.ResponseWriter, r *http.Request) {

	sc, ok := gache.SGet(r) // get server side cache

	if ok {
		w.WriteHeader(sc.Status)
		w.Write(sc.Body)
		return
	}

	var body []byte

	/**
	*  do something
	*/

	go func() {
		err := gache.SSet(r, http.StatusOK, nil, body) // set server side cache
		if err != nil {
			log.Println(err)
		}
	}()

	w.Write(body)
}
```

### Client-Side Cache Example
```go
	req, err := http.NewRequest(http.MethodGet, "https://github.com/kpango/gache", nil)

	if err != nil{
		// some err handling
	}

	var res *http.Response

	cache, ok := gache.CGet(req)

	if ok {
		res = cache.Res
	}else{
		res, err = http.DefaultClient.Do(req)
		err = gache.CSet(req, res)
	}

```

## Benchmarks

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
