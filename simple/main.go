package main

import (
	"github.com/BlockABC/cache_module"
	"github.com/BlockABC/cache_module/redis"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func main() {
	cacheClient := redis.New("127.0.0.1:6379", "", 0, time.Millisecond*100)
	router := gin.New()
	cacheMiddleware := cache.NewCacheMiddleware(nil, cacheClient, true)

	router.GET("/test", cacheMiddleware.CacheGet(30, cache.Redis), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"errno": 0, "errmsg": "Success", "data": gin.H{"symbol_list": gin.H{"symbol": "EOS", "code": "eosio.token", "balance": "2.7937"}}})
	})
	_ = router.Run(":8080")
}
