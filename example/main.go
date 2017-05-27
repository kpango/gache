package main

import (
	"log"
	"net/http"
	"time"

	"github.com/kpango/gache"
	"github.com/kpango/glog"
)

func main() {

	/**
	simple cache example
	*/
	var (
		key1 = "key"
		key2 = 5050
		key3 = struct{}{}

		value1 = "value"
		value2 = 88888
		value3 = struct{}{}
	)
	// store plain cache default expire is 30 Seconds
	gache.Set(key1, value3)
	gache.Set(key2, value2)
	gache.Set(key3, value1)

	// set gache default expire time
	gache.SetDefaultExpire(time.Second * 10)

	// store with expire setting
	gache.SetWithExpire(key1, value3, time.Second*30)
	gache.SetWithExpire(key2, value2, time.Second*60)
	gache.SetWithExpire(key3, value1, time.Hour)

	// get cache data
	gache.Get(key1)
	gache.Get(key2)
	gache.Get(key3)

	/**
	server side handler cache example
	*/
	http.Handle("/", glog.HTTPLogger("sample", httpServerExample))

	/**
	http client side cache example
	*/
	httpClientExample()

	http.ListenAndServe(":9090", nil)
}

// httpServerExample is server side handler cache example
func httpServerExample(w http.ResponseWriter, r *http.Request) {

	sc, ok := gache.SGet(r) // get server side cache

	// if cached data already exist, return cached data
	if ok {
		/*
			sc contains the following members
				Status int
				Header http.Header
				Body   []byte
		*/
		w.WriteHeader(sc.Status)
		w.Write(sc.Body)
		return
	}

	var body []byte

	// TODO: do something

	// store the response data to cache
	go func() {
		err := gache.SSet(r, http.StatusOK, nil, body) // set server side cache

		if err != nil {
			log.Println(err)
		}
	}()

	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// httpClientExample is http client side cache example
func httpClientExample() *http.Response {

	req, err := http.NewRequest(http.MethodGet, "https://github.com/kpango", nil)

	if err != nil {
		log.Println(err)
		return nil
	}

	// get client side cache
	cc, ok := gache.CGet(req)

	var res *http.Response

	// if cached data already exist, return cached data
	if ok {
		return cc.Res
	} else {
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			log.Println(err)
			return nil
		}
		// store client side response data to cache
		gache.CSet(req, res)
	}
	return res
}
