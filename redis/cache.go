package redis

import (
	"fmt"
	"github.com/go-redis/redis"
	"time"
)

func New(addr, pass string, db int, timeOut ...time.Duration) *redis.Client {
	opt := &redis.Options{
		Addr:        addr,
		Password:    pass,
		DB:          db,
		PoolSize:    10,
		PoolTimeout: 30 * time.Second,
	}
	switch len(timeOut) {
	case 3:
		fmt.Println("init write timeout:", timeOut[2])
		opt.WriteTimeout = timeOut[2]
		fallthrough
	case 2:
		fmt.Println("init read timeout:", timeOut[1])
		opt.ReadTimeout = timeOut[1]
		fallthrough
	case 1:
		fmt.Println("init dial timeout:", timeOut[0])
		opt.DialTimeout = timeOut[0]
	}
	return redis.NewClient(opt)
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
