package gache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type (
	// Gache is base instance type
	Gache struct {
		mu     *sync.RWMutex
		data   *sync.Map
		expire time.Duration
	}

	value struct {
		expire int64
		val    *interface{}
	}

	ServerCache struct {
		Status int
		Header http.Header
		Body   []byte
	}

	ClientCache struct {
		Etag         string
		expire       time.Time
		LastModified string
		Res          *http.Response
	}
)

var (
	gache *Gache
	once  sync.Once
)

func init() {
	GetGache()
}

func New() *Gache {
	return &Gache{
		mu:     new(sync.RWMutex),
		data:   new(sync.Map),
		expire: time.Second * 30,
	}
}

func GetGache() *Gache {
	once.Do(func() {
		gache = New()
	})
	return gache
}

func (v value) isValid() bool {
	return time.Now().UnixNano() < v.expire
}

func SetDefaultExpire(ex time.Duration) {
	gache.SetDefaultExpire(ex)
}

func (g *Gache) SetDefaultExpire(ex time.Duration) *Gache {
	defer g.mu.Unlock()
	g.mu.Lock()
	g.expire = ex
	return g
}

func (g *Gache) StartExpired(ctx context.Context, dur time.Duration) *Gache {
	go func() {
		tick := time.NewTicker(dur)
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				return
			case <-tick.C:
				g.DeleteExpired()
			}
		}
	}()
	return g
}

func Get(key interface{}) (interface{}, bool) {
	return gache.get(key)
}

func (g *Gache) Get(key interface{}) (interface{}, bool) {
	return g.get(key)
}

func (g *Gache) get(key interface{}) (interface{}, bool) {

	v, ok := g.data.Load(key)

	if !ok {
		return nil, false
	}

	d, ok := v.(*value)

	if !ok || !d.isValid() {
		g.data.Delete(key)
		return nil, false
	}

	return *d.val, true
}

func SetWithExpire(key, val interface{}, expire time.Duration) {
	gache.set(key, val, expire)
}

func Set(key, val interface{}) {
	gache.set(key, val, gache.expire)
}

func (g *Gache) SetWithExpire(key, val interface{}, expire time.Duration) {
	g.set(key, val, expire)
}

func (g *Gache) Set(key, val interface{}) {
	g.set(key, val, g.expire)
}

func (g *Gache) set(key, val interface{}, expire time.Duration) {
	g.data.Store(key, &value{
		expire: time.Now().Add(expire).UnixNano(),
		val:    &val,
	})
}

func (g *Gache) DeleteExpired() int {
	var rows int
	g.data.Range(func(k, v interface{}) bool {
		d, ok := v.(*value)
		if ok && !d.isValid() {
			g.data.Delete(k)
			rows++
		}
		return true
	})
	return rows
}

func (g *Gache) Delete(key interface{}) bool {
	g.data.Delete(key)
	return false
}

func (g *Gache) SGet(key *http.Request) (*ServerCache, bool) {
	return g.getServerCache(key)
}

func (g *Gache) SSetWithExpire(key *http.Request, status int, header http.Header, body []byte, expire time.Duration) error {
	return g.setServerCache(key, status, header, body, expire)
}

func (g *Gache) SSet(key *http.Request, status int, header http.Header, body []byte) error {
	return g.setServerCache(key, status, header, body, g.expire)
}

func (g *Gache) CGet(key *http.Request) (*ClientCache, bool) {
	return g.getClientCache(key)
}

func (g *Gache) CSet(key *http.Request, val *http.Response) error {
	return g.setClientCache(key, val)
}

func SGet(key *http.Request) (*ServerCache, bool) {
	return gache.getServerCache(key)
}

func SSetWithExpire(key *http.Request, status int, header http.Header, body []byte, expire time.Duration) error {
	return gache.setServerCache(key, status, header, body, expire)
}

func SSet(key *http.Request, status int, header http.Header, body []byte) error {
	return gache.setServerCache(key, status, header, body, gache.expire)
}

func CGet(key *http.Request) (*ClientCache, bool) {
	return gache.getClientCache(key)
}

func CSet(key *http.Request, val *http.Response) error {
	return gache.setClientCache(key, val)
}

func (g *Gache) getServerCache(req *http.Request) (*ServerCache, bool) {
	key := generateHTTPKey(req)

	cache, ok := g.get(key)

	if !ok {
		return nil, false
	}

	return cache.(*ServerCache), ok
}

func (g *Gache) setServerCache(req *http.Request, status int, header http.Header, body []byte, expire time.Duration) error {

	key := generateHTTPKey(req)

	_, ok := g.get(key)
	if ok {
		return errors.New("cache already exists")
	}

	g.set(key, &ServerCache{
		Status: status,
		Header: header,
		Body:   body,
	}, expire)

	return nil
}

func (g *Gache) getClientCache(req *http.Request) (*ClientCache, bool) {
	key := generateHTTPKey(req)
	data, ok := g.get(key)
	if !ok {
		return nil, false
	}
	return data.(*ClientCache), true
}

func (g *Gache) setClientCache(req *http.Request, val *http.Response) error {
	key := generateHTTPKey(req)
	_, ok := g.get(key)
	if ok {
		return errors.New("cache already exists")
	}

	cache, err := createHTTPCache(val)

	if err != nil {
		return err
	}

	g.set(key, cache, time.Until(cache.expire))
	return nil
}

func Foreach(f func(key, val interface{}, expire int64) bool) *Gache {
	return gache.Foreach(f)
}

func (g *Gache) Foreach(f func(key, val interface{}, expire int64) bool) *Gache {
	g.data.Range(func(key, val interface{}) bool {
		d, ok := val.(*value)
		if !ok {
			return f(key, val, d.expire)
		}
		return false
	})
	return g
}

func (g *Gache) Clear() {
	g.data.Range(func(key, val interface{}) bool {
		g.data.Delete(key)
		return true
	})
	g.data = nil
}

func Clear() {
	gache.Clear()
}

func generateHTTPKey(r *http.Request) string {
	return fmt.Sprintf("%s%s%s%s%v", r.RequestURI, r.Proto, r.Host, r.Method, r.Body)
}

func createHTTPCache(res *http.Response) (*ClientCache, error) {

	header := res.Header.Get("Cache-Control")
	if len(header) == 0 {
		return nil, errors.New("Cache-Control Header Not Found")
	}

	header = strings.Trim(header, " ")

	if strings.Contains(header, "no-store") || !strings.Contains(header, "max-age") {
		return nil, errors.New("cache disabled")
	}

	t, err := strconv.Atoi(strings.Split(strings.Split(header, "max-age=")[1], ",")[0])

	if err != nil {
		return nil, errors.New("Invalid max-age format")
	}

	return &ClientCache{
		LastModified: res.Header.Get("Last-Modified"),
		Etag:         res.Header.Get("ETag"),
		expire:       time.Now().Add(time.Duration(t) * time.Second),
		Res:          res,
	}, nil
}
