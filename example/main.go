package main

import (
	"time"

	"github.com/kpango/gache"
	"github.com/kpango/glg"
)

func main() {
	var (
		key1   = "key1"
		key2   = "key2"
		key3   = "key3"
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
	gc := gache.New().SetDefaultExpire(time.Second * 10)

	// store with expire setting
	gc.SetWithExpire(key1, value1, time.Second*30)
	gc.SetWithExpire(key2, value2, time.Second*60)
	gc.SetWithExpire(key3, value3, time.Hour)

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
