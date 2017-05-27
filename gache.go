package gache

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/syncmap"
)

type Cache struct {
	mu            *sync.RWMutex
	Data          *syncmap.Map
	defaultExpire time.Duration
}

type value struct {
	expire time.Time
	val    interface{}
}

type ServerCache struct {
	Status int
	Header http.Header
	Body   []byte
}

type ClientCache struct {
	Etag         string
	Expire       time.Time
	LastModified string
	Res          *http.Response
}

const (
	defaultExpire = time.Second * 30
)

var (
	instance *Cache
	once     sync.Once

	cacheRegex = regexp.MustCompile(`([a-zA-Z][a-zA-Z_-]*)\s*(?:=(?:"([^"]*)"|([^ \t",;]*)))?`)
)

func init() {
	instance = GetCache().SetDefauleExpire(defaultExpire)
}

func (c *Cache) SetDefaultExpire(ex time.Duration) *Cache {
	defer c.mu.Unlock()
	c.mu.Lock()
	c.defaultExpire = ex
	return c
}

func Get(key interface{}) (interface{}, bool) {
	return instance.get(key)
}

func (c *Cache) get(key interface{}) (interface{}, bool) {
	v, ok := c.Data.Load(key)

	if !ok {
		return nil, false
	}

	if v == nil || !time.Now().Before(v.(value).expire) {
		c.Data.Delete(key)
		return nil, false
	}

	return v.(value).val, true
}

func SetWithExpire(key, value interface{}, expire time.Duration) bool {
	return instance.set(key, value, expire)
}

func Set(key, value interface{}) bool {
	return instance.set(key, value, instance.defaultExpire)
}

func (c *Cache) set(key, val interface{}, expire time.Duration) bool {
	v, ok := c.Data.Load(key)

	if ok && time.Now().Before(v.(value).expire) {
		return false
	}

	c.Data.Store(key, value{
		expire: time.Now().Add(expire),
		val:    val,
	})

	return true
}

func GetCache() *Cache {
	once.Do(func() {
		instance = &Cache{
			mu:            new(sync.RWMutex),
			Data:          new(syncmap.Map),
			defaultExpire: time.Second * 30,
		}
	})
	return instance
}

func SGet(key *http.Request) (*ServerCache, bool) {
	return instance.getServerCache(key)
}

func SSetWithExpire(key *http.Request, status int, header http.Header, body []byte, expire time.Duration) error {
	return instance.setServerCache(key, status, header, body, expire)
}

func SSet(key *http.Request, status int, header http.Header, body []byte) error {
	return instance.setServerCache(key, status, header, body, instance.defaultExpire)
}

func CGet(key *http.Request) (*ClientCache, bool) {
	return instance.getClientCache(key)
}

func CSet(key *http.Request, val *http.Response) error {
	return instance.setClientCache(key, val)
}

func (c *Cache) getServerCache(key *http.Request) (*ServerCache, bool) {
	cache, ok := c.get(key)
	if !ok {
		return nil, false
	}
	return cache.(value).val.(*ServerCache), ok
}

func (c *Cache) setServerCache(key *http.Request, status int, header http.Header, body []byte, expire time.Duration) error {

	_, ok := c.get(key)
	if ok {
		return errors.New("cache already exists")
	}

	if !c.set(key, &ServerCache{
		Status: status,
		Header: header,
		Body:   body,
	}, expire) {
		return errors.New("cache already exists")
	}

	return nil
}

func (c *Cache) getClientCache(key *http.Request) (*ClientCache, bool) {
	data, ok := c.get(key)
	if !ok {
		return nil, false
	}
	return data.(value).val.(*ClientCache), true
}

func (c *Cache) setClientCache(key *http.Request, val *http.Response) error {
	_, ok := c.getClientCache(key)
	if ok {
		return errors.New("cache already exists")
	}

	cache, err := createHTTPCache(val)

	if err != nil {
		return err
	}

	if !c.set(key, cache, cache.Expire.Sub(time.Now())) {
		return errors.New("cache already exists")
	}

	return nil
}

func (c *Cache) Clear() {
	c.Data.Range(func(key, val interface{}) bool {
		c.Data.Delete(key)
		return true
	})
	c.Data = nil
}

func createHTTPCache(res *http.Response) (*ClientCache, error) {

	header := res.Header.Get("Cache-Control")
	if len(header) == 0 {
		return nil, errors.New("Cache-Control Header Not Found")
	}

	cache := make(map[string]string)

	for _, match := range cacheRegex.Copy().FindAllString(header, -1) {
		if strings.EqualFold(match, "no-store") {
			return nil, errors.New("no-store detected")
		}
		var key, value string
		key = match
		if index := strings.Index(match, "="); index != -1 {
			key, value = match[:index], match[index+1:]
		}
		cache[key] = value
	}

	limit, ok := cache["max-age"]

	if !ok {
		return nil, errors.New("cache age not found")
	}

	t, err := strconv.Atoi(limit)

	if err != nil {
		return nil, err
	}

	return &ClientCache{
		LastModified: res.Header.Get("Last-Modified"),
		Etag:         res.Header.Get("ETag"),
		Expire:       time.Now().Add(time.Duration(t) * time.Second),
		Res:          res,
	}, nil
}
