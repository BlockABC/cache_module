package redis

import (
	"github.com/go-redis/redis"
)

func New(addr, pass string, db int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: pass,
		DB:       db,
	})
}

func NewOptions(options *redis.Options) *redis.Client {
	return redis.NewClient(options)
}
