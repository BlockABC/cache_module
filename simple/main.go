package main

import (
	"github.com/BlockABC/cache_module"
	"github.com/BlockABC/cache_module/redis"
	"github.com/gin-gonic/gin"
	"net/http"
)

func main() {
	cacheClient := redis.New("127.0.0.1:6379", "", 0)
	router := gin.New()
	cacheMiddleware := cache.NewCacheMiddleware(nil, cacheClient, true)

	router.POST("/test", cacheMiddleware.CachePOST(30, cache.Redis), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"errno": 0, "errmsg": "Success", "data": gin.H{"symbol_list": gin.H{"symbol": "EOS", "code": "eosio.token", "balance": "2.7937"}}})
	})

	router.Run(":8080")

}
