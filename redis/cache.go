package redis

import (
	"github.com/go-redis/redis"
)

type Client struct {
	Client *redis.Client
}

func New(addr, pass string, db int) *Client {
	cache := Client{}
	cache.Client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: pass,
		DB:       db,
	})
	return &cache
}



func NewOptions(options *redis.Options) *Client {
	cache := Client{}
	cache.Client = redis.NewClient(options)
	return &cache
}
