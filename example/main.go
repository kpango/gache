package main

import (
	"log"
	"net/http"
	"time"

	"github.com/kpango/gache"
	"github.com/kpango/glg"
)

func main() {

	/**
	simple cache example
	*/
	simpleExample()

	/**
	server side handler cache example
	*/
	http.Handle("/", glg.HTTPLoggerFunc("sample", httpServerExample))

	/**
	http client side cache example
	*/
	httpClientExample()

	http.ListenAndServe(":9090", nil)
}

//	simple cache example
func simpleExample() {
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
	// get cache data
	v1, ok := gache.Get(key1)
	if ok {
		glg.Info(v1)
	}
	v2, ok := gache.Get(key2)
	if ok {
		glg.Info(v2)
	}
	v3, ok := gache.Get(key3)
	if ok {
		glg.Info(v3)
	}

	// set gache default expire time
	gc := gache.New().SetDefaultexpire(time.Second * 10)

	// store with expire setting
	gc.SetWithexpire(key1, value1, time.Second*30)
	gc.SetWithexpire(key2, value2, time.Second*60)
	gc.SetWithexpire(key3, value3, time.Hour)

	// get cache data
	v4, ok := gc.Get(key1)
	if ok {
		glg.Info(v4)
	}
	v5, ok := gc.Get(key2)
	if ok {
		glg.Info(v5)
	}
	v6, ok := gc.Get(key3)
	if ok {
		glg.Info(v6)
	}
}

// httpServerExample is server side handler cache example
func httpServerExample(w http.ResponseWriter, r *http.Request) {

	sc, ok := gache.SGet(r) // get server side cache

	// if cached data already exist, return cached data
	if ok {
		glg.Info("cached Response")
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
	body = []byte("Hello gache Cache Sample")

	// store the response data to cache
	go func() {
		err := gache.SSet(r, http.StatusOK, nil, body) // set server side cache

		if err != nil {
			log.Println(err)
		}
		glg.Info("Response cached")
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
		/*
			sc contains the following members
				Etag         string
				expire       time.Time
				LastModified string
				Res          *http.Response
		*/
		return cc.Res
	}

	res, err = http.DefaultClient.Do(req)
	if err != nil {
		log.Println(err)
		return nil
	}
	// store client side response data to cache
	go gache.CSet(req, res)

	return res
}
