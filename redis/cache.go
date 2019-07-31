package redis

import (
	"github.com/go-redis/redis"
	"time"
)

func New(addr, pass string, db int, timeOut time.Duration) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     pass,
		DB:           db,
		DialTimeout:  timeOut,
		ReadTimeout:  timeOut,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		PoolTimeout:  30 * time.Second,
	})
}

func NewOptions(options *redis.Options) *redis.Client {
	return redis.NewClient(options)
}

func IsAlive(r *redis.Client) bool {
	select {
	case <-time.After(time.Millisecond * 100):
		return false
	default:
		ret := r.Ping()
		return ret.Err() == nil
	}
}
