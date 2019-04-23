# cache


## 使用

```sh
go get github.com/BlockABC/cache

```

## Demo  main.go

```go
package main

import (
	"github.com/BlockABC/cache"
	"github.com/BlockABC/cache/redis"
	"github.com/gin-gonic/gin"
	"net/http"
)

func main() {
    //初始化redis
	cacheClient := redis.New("127.0.0.1:6379", "", 0)
	//gin 相关
	router := gin.New()
	// 初始化缓存中间件 1，MemCache client 2,RedisCache 3,Whether to use default false
	cacheMiddleware := cache.NewCacheMiddleware(nil, cacheClient, true)

    // cacheMiddleware.CacheGET(30, cache.Redis) 1，缓存时间 2，缓存类型Redis or MemCache
	router.GET("/test", cacheMiddleware.CacheGET(30, cache.Redis), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"errno": 0, "errmsg": "Success", "data": gin.H{"symbol_list": gin.H{"symbol": "EOS", "code": "eosio.token", "balance": "2.7937"}}})
	})

	router.Run(":8080")

}

```
[![Build Status](https://travis-ci.org/gin-gonic/gin.svg)](https://travis-ci.org/gin-gonic/gin)



## 使用12个线程运行30秒, 400个http并发

```sh
wrk -t12 -c400 -d30s http://127.0.0.1:8080/test




Running 30s test @ http://127.0.0.1:8080/test
  12 threads and 400 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency    25.16ms    6.88ms  98.51ms   89.21%
    Req/Sec   803.18    575.34     2.77k    41.18%
  287745 requests in 30.10s, 64.21MB read
  Socket errors: connect 155, read 0, write 0, timeout 0
Requests/sec:   9560.67
Transfer/sec:      2.13MB

```


