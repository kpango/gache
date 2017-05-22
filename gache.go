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
	mu            sync.RWMutex
	Data          *syncmap.Map
	defaultExpire time.Duration
}

type ServerCache struct {
	Expire time.Time
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
	instance = GetCache().SetDefauleExpite(defaultExpire)
}

func GetCache() *Cache {
	once.Do(func() {
		instance = &Cache{
			Data:          new(syncmap.Map),
			defaultExpire: time.Second * 30,
		}
	})
	return instance
}

func (c *Cache) SetDefauleExpite(ex time.Duration) *Cache {
	defer c.mu.Unlock()
	c.mu.Lock()
	c.defaultExpire = ex
	return c
}

func SGet(key *http.Request) (*ServerCache, bool) {
	return GetCache().getServerCache(key)
}

func SSet(key *http.Request, status int, header http.Header, body []byte) error {
	return GetCache().setServerCache(key, status, header, body)
}

func CGet(key *http.Request) (*ClientCache, bool) {
	return GetCache().getClientCache(key)
}

func CSet(key *http.Request, val *http.Response) error {
	return GetCache().setClientCache(key, val)
}

func (c *Cache) getServerCache(key *http.Request) (*ServerCache, bool) {
	data, ok := c.Data.Load(key)

	if !ok || !time.Now().Before(data.(*ServerCache).Expire) {
		c.Data.Delete(key)
		return nil, false
	}

	return data.(*ServerCache), true
}

func (c *Cache) setServerCache(key *http.Request, status int, header http.Header, body []byte) error {
	data, ok := c.Data.Load(key)

	if ok && time.Now().Before(data.(*ServerCache).Expire) {
		return errors.New("cache already exists")
	}

	c.Data.Store(key, &ServerCache{
		Expire: time.Now().Add(c.defaultExpire),
		Status: status,
		Header: header,
		Body:   body,
	})

	return nil
}

func (c *Cache) getClientCache(key *http.Request) (*ClientCache, bool) {
	data, ok := c.Data.Load(key)
	if !ok || !time.Now().Before(data.(ClientCache).Expire) {
		c.Data.Delete(key)
		return nil, false
	}
	return data.(*ClientCache), true
}

func (c *Cache) setClientCache(key *http.Request, val *http.Response) error {
	data, ok := c.Data.Load(key)
	if ok && time.Now().Before(data.(ClientCache).Expire) {
		return errors.New("")
	}

	cache, err := createHTTPCache(val)

	if err != nil {
		return err
	}

	c.Data.Store(key, cache)

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
