package memche

import (
	"github.com/bradfitz/gomemcache/memcache"
)

type Client struct {
	Client *memcache.Client
}

func New(cacheSvrList []string) *Client {
	cache := Client{}
	cache.Client = memcache.New(cacheSvrList...)
	return &cache
}

func (cache *Client) Set(key string, value []byte, expiration int32) error {
	return cache.Client.Set(&memcache.Item{Key: key, Value: value, Expiration: expiration})
}

func (cache *Client) Get(key string) ([]byte, error) {
	item, err := cache.Client.Get(key)
	if err != nil {
		return nil, err
	}
	return item.Value, nil
}
