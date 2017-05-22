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
	Data          *syncmap.Map
	defaultExpire time.Duration
}

type CacheData struct {
	Etag         string
	Expire       time.Time
	LastModified string
	Res          *http.Response
}

var (
	instance *Cache
	once     sync.Once

	cacheRegex = regexp.MustCompile(`([a-zA-Z][a-zA-Z_-]*)\s*(?:=(?:"([^"]*)"|([^ \t",;]*)))?`)
)

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
	c.defaultExpire = ex
	return c
}

func (c *Cache) Get(key *http.Request) (*CacheData, bool) {
	data, ok := c.Data.Load(key)
	if !ok || !time.Now().Before(data.(CacheData).Expire) {
		c.Data.Delete(key)
		return nil, false
	}
	return data.(*CacheData), true
}

func (c *Cache) Set(key *http.Request, val *http.Response) error {
	data, ok := c.Data.Load(key)
	if ok && time.Now().Before(data.(CacheData).Expire) {
		return errors.New("")
	}

	cache, err := createHTTPCache(val)

	if err != nil {
		t := time.Now().Add(c.defaultExpire)
		cache = &CacheData{
			Res:          val,
			Expire:       t,
			LastModified: t.String(),
		}
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

func createHTTPCache(res *http.Response) (*CacheData, error) {

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

	return &CacheData{
		LastModified: res.Header.Get("Last-Modified"),
		Etag:         res.Header.Get("ETag"),
		Expire:       time.Now().Add(time.Duration(t) * time.Second),
		Res:          res,
	}, nil
}
