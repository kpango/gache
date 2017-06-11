package gache

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/syncmap"
)

type (
	Gache struct {
		mu     *sync.RWMutex
		data   *syncmap.Map
		expire time.Duration
	}

	value struct {
		expire time.Time
		val    *interface{}
	}

	ServerCache struct {
		Status int
		Header http.Header
		Body   []byte
	}

	ClientCache struct {
		Etag         string
		Expire       time.Time
		LastModified string
		Res          *http.Response
	}
)

var (
	instance   *Gache
	once       sync.Once
	cacheRegex = regexp.MustCompile(`([a-zA-Z][a-zA-Z_-]*)\s*(?:=(?:"([^"]*)"|([^ \t",;]*)))?`)
)

func init() {
	GetGache()
}

func New() *Gache {
	return &Gache{
		mu:     new(sync.RWMutex),
		data:   new(syncmap.Map),
		expire: time.Second * 30,
	}
}

func GetGache() *Gache {
	once.Do(func() {
		instance = New()
	})
	return instance
}

func (v value) isValid() bool {
	return time.Now().Before(v.expire)
}

func (g *Gache) SetDefaultExpire(ex time.Duration) *Gache {
	defer g.mu.Unlock()
	g.mu.Lock()
	g.expire = ex
	return g
}

func SetDefaultExpire(ex time.Duration) {
	defer instance.mu.Unlock()
	instance.mu.Lock()
	instance.expire = ex
}

func Get(key interface{}) (interface{}, bool) {
	return instance.get(key)
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

func SetWithExpire(key, val interface{}, expire time.Duration) bool {
	return instance.set(key, val, expire)
}

func Set(key, val interface{}) bool {
	return instance.set(key, val, instance.expire)
}

func (g *Gache) SetWithExpire(key, val interface{}, expire time.Duration) bool {
	return g.set(key, val, expire)
}

func (g *Gache) Set(key, val interface{}) bool {
	return g.set(key, val, g.expire)
}

func (g *Gache) set(key, val interface{}, expire time.Duration) bool {
	g.data.Store(key, &value{
		expire: time.Now().Add(expire),
		val:    &val,
	})

	return true
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
	return instance.getServerCache(key)
}

func SSetWithExpire(key *http.Request, status int, header http.Header, body []byte, expire time.Duration) error {
	return instance.setServerCache(key, status, header, body, expire)
}

func SSet(key *http.Request, status int, header http.Header, body []byte) error {
	return instance.setServerCache(key, status, header, body, instance.expire)
}

func CGet(key *http.Request) (*ClientCache, bool) {
	return instance.getClientCache(key)
}

func CSet(key *http.Request, val *http.Response) error {
	return instance.setClientCache(key, val)
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

	if !g.set(key, &ServerCache{
		Status: status,
		Header: header,
		Body:   body,
	}, expire) {
		return errors.New("cache already exists")
	}

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

	if !g.set(key, cache, cache.Expire.Sub(time.Now())) {
		return errors.New("cache already exists")
	}

	return nil
}

func (g *Gache) Clear() {
	g.data.Range(func(key, val interface{}) bool {
		g.data.Delete(key)
		return true
	})
	g.data = nil
}

func Clear() {
	instance.Clear()
}

func generateHTTPKey(r *http.Request) string {
	key := fmt.Sprintf("%s%s%s%s%v", r.RequestURI, r.Proto, r.Host, r.Method, r.Body)
	for _, v := range r.Cookies() {
		key += v.String()
	}
	return key
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
		Expire:       time.Now().Add(time.Duration(t) * time.Second),
		Res:          res,
	}, nil
}
