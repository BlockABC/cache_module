package main

import (
	"github.com/BlockABC/cache/middleware"
	"github.com/BlockABC/cache/redis"
	"github.com/gin-gonic/gin"
	"net/http"
)

func main() {
	cacheClient := redis.New("127.0.0.1:6379", "", 0)
	router := gin.New()
	cacheMiddleware := webservermiddleware.NewCacheMiddleware(nil, cacheClient, true)

	router.GET("/test", cacheMiddleware.CacheGET(30, webservermiddleware.REDIS), func(c *gin.Context) {
		//TODO
		c.JSON(http.StatusOK, gin.H{"errno": 0, "errmsg": "Success", "data": gin.H{"symbol_list": gin.H{"symbol": "EOS","code": "eosio.token","balance": "2.7937"}}})
	})

	router.Run(":8080")

}
