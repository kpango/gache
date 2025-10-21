package main

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"time"
	"unsafe"

	"github.com/kpango/gache/v2"
	"github.com/kpango/glg"
)

var (
	bigData      = map[string]string{}
	bigDataLen   = 2 << 10
	bigDataCount = 2 << 11
)

func init() {
	for i := 0; i < bigDataCount; i++ {
		bigData[randStr(bigDataLen)] = randStr(bigDataLen)
	}
}

var randSrc = rand.NewSource(time.Now().UnixNano())

const (
	rs6Letters       = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	rs6LetterIdxBits = 6
	rs6LetterIdxMask = 1<<rs6LetterIdxBits - 1
	rs6LetterIdxMax  = 63 / rs6LetterIdxBits
)

func randStr(n int) string {
	b := make([]byte, n)
	cache, remain := randSrc.Int63(), rs6LetterIdxMax
	for i := n - 1; i >= 0; {
		if remain == 0 {
			cache, remain = randSrc.Int63(), rs6LetterIdxMax
		}
		idx := int(cache & rs6LetterIdxMask)
		if idx < len(rs6Letters) {
			b[i] = rs6Letters[idx]
			i--
		}
		cache >>= rs6LetterIdxBits
		remain--
	}
	return *(*string)(unsafe.Pointer(&b))
}

func main() {
	var (
		key1   = "key1"
		key2   = "key2"
		key3   = "key3"
		value1 = "value"
		value2 = 88888
		value3 = struct{}{}
	)

	gc := gache.New[any]().SetDefaultExpire(time.Second*10).StartExpired(context.Background(), time.Hour)

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

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mbody, err := json.Marshal(m)
	if err == nil {
		glg.Debugf("memory size: %d, lenght: %d, mem stats: %v", gc.Size(), gc.Len(), string(mbody))
	}
	path := "/tmp/gache-sample.gdb"

	file, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		glg.Error(err)
		return
	}
	gob.Register(struct{}{})
	err = gc.Write(context.Background(), file)
	gc.Stop()
	file.Close()
	if err != nil {
		glg.Error(err)
		return
	}

	gcn := gache.New[any]().SetDefaultExpire(time.Minute)
	file, err = os.OpenFile(path, os.O_RDONLY, 0o755)
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

	runtime.GC()
	gch := gache.New[int64]().EnableExpiredHook().
		SetExpiredHook(func(ctx context.Context, key string, v int64) {
			glg.Debugf("key=%v value=%d expired", key, v)
		}).
		StartExpired(context.Background(), time.Second*10)
	for i := 0; i < 10000; i++ {
		gch.SetWithExpire("sample-"+strconv.Itoa(i), int64(i), time.Second*5)
	}
	time.Sleep(time.Second * 20)
	glg.Debugf("length: %d", gch.Len())

	runtime.GC()
	gcs := gache.New[string]()
	maxCnt := 10000000
	digitLen := len(strconv.Itoa(maxCnt))
	for i := 0; i < maxCnt; i++ {
		if i%1000 == 0 {
			// runtime.ReadMemStats(&m)
			// mbody, err := json.Marshal(m)
			if err == nil {
				// glg.Debugf("before set memory size: %d, lenght: %d, mem stats: %v", gcs.Size(), gcs.Len(), string(mbody))
				glg.Debugf("Execution No.%-*d:\tbefore set memory size: %d, lenght: %d", digitLen, i, gcs.Size(), gcs.Len())
			}
		}
		for k, v := range bigData {
			gcs.Set(k, v)
		}
		if i%1000 == 0 {
			// runtime.ReadMemStats(&m)
			// mbody, err := json.Marshal(m)
			if err == nil {
				glg.Debugf("Execution No.%-*d:\tafter set memory size: %d, lenght: %d", digitLen, i, gcs.Size(), gcs.Len())
				// glg.Debugf("after set memory size: %d, lenght: %d, mem stats: %v", gcs.Size(), gcs.Len(), string(mbody))
			}
		}

		for k := range bigData {
			gcs.Get(k)
		}
		for k := range bigData {
			gcs.Delete(k)
		}
		if i%1000 == 0 {
			// runtime.ReadMemStats(&m)
			// mbody, err := json.Marshal(m)
			if err == nil {
				glg.Debugf("Execution No.%-*d:\tafter delete memory size: %d, lenght: %d", digitLen, i, gcs.Size(), gcs.Len())
				// glg.Debugf("after delete memory size: %d, lenght: %d, mem stats: %v", gcs.Size(), gcs.Len(), string(mbody))
			}
			runtime.GC()
			// runtime.ReadMemStats(&m)
			// mbody, err = json.Marshal(m)
			if err == nil {
				glg.Debugf("Execution No.%-*d:\tafter gc memory size: %d, lenght: %d", digitLen, i, gcs.Size(), gcs.Len())
				// glg.Debugf("after gc memory size: %d, lenght: %d, mem stats: %v", gcs.Size(), gcs.Len(), string(mbody))
			}
		}

	}
}
