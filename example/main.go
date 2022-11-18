package main

import (
	"context"
	"os"
	"time"

	"github.com/kpango/gache/v2"
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

	gc := gache.New[any]().SetDefaultExpire(time.Second * 10)

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

	gc.Range(context.Background(), func(k string, v any, exp int64) bool {
		glg.Debugf("key:\t%v\nval:\t%v", k, v)
		return true
	})

	file, err := os.OpenFile("/tmp/gache-sample.gdb", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		glg.Error(err)
		return
	}
	gc.Write(context.Background(), file)

	file.Close()

	gcn := gache.New[any]().SetDefaultExpire(time.Minute)
	file, err = os.OpenFile("/tmp/gache-sample.gdb", os.O_RDONLY, 0o755)
	if err != nil {
		glg.Error(err)
		return
	}

	err = gcn.Read(file)

	file.Close()

	if err != nil {
		glg.Error(err)
		return
	}

	gcn.Range(context.Background(), func(k string, v interface{}, exp int64) bool {
		glg.Warnf("key:\t%v\nval:\t%v", k, v)
		return true
	})

	// instantiage new gache for int64 type as gci
	gci := gache.New[int64]()

	gci.Set("sample1", int64(0))
	gci.Set("sample2", int64(10))
	gci.Set("sample3", int64(100))

	// gache supports range loop processing method and inner function argument is int64 as contract
	gci.Range(context.Background(), func(k string, v int64, exp int64) bool {
		glg.Debugf("key:\t%v\nval:\t%d", k, v)
		return true
	})
}
