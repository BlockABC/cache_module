package webservermiddleware

import (
	"bytes"
	"encoding/json"
	"github.com/BlockABC/cache/memche"
	"github.com/BlockABC/cache/module"
	"github.com/BlockABC/cache/redis"
	"github.com/BlockABC/cache/util"
	"io/ioutil"
	"log"
	"net/http"

	"fmt"
	gouache "github.com/bradfitz/gomemcache/memcache"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

const (
	REQUEST_UNLOCK = "0"
	REQUEST_LOCK   = "1"

	MEMCACHE = 1
	REDIS    = 2
	LOCK = "lock:"
	REQUEST_CACHE_TIME = "request_cache_time:"
)

type cacheKeyGetter = func(c *gin.Context) string
type shouldCacheHandler = func(apiResp *module.ApiResp) bool

type CacheMiddleware struct {
	cacheClientMemcache *memche.Client
	cacheClientRedis    *redis.Client
	enableCache         bool
}

func NewCacheMiddleware(cacheClientMemcache *memche.Client, cacheClientRedis *redis.Client, enableCache bool) *CacheMiddleware {
	middleware := CacheMiddleware{
		cacheClientMemcache: cacheClientMemcache,
		cacheClientRedis:    cacheClientRedis,
		enableCache:         enableCache,
	}
	return &middleware
}

func (m *CacheMiddleware) CacheGET(cacheTime, cacheType int32) gin.HandlerFunc {
	switch cacheType {
	case MEMCACHE:
		return cacheGetByMemcache(cacheTime, m)
	case REDIS:
		return cacheGetByRedis(cacheTime, m)
	default:
		return cachePostByRedis(cacheTime, m)
	}
}

func (m *CacheMiddleware) CachePOST(cacheTime, cacheType int32) gin.HandlerFunc {
	switch cacheType {
	case MEMCACHE:
		return cachePostByMemcache(cacheTime, m)
	case REDIS:
		return cachePostByRedis(cacheTime, m)
	default:
		return cachePostByRedis(cacheTime, m)
	}
}

func cacheGetByMemcache(cacheTime int32, m *CacheMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			return
		}

		cacheRequestByMemcache(
			m, cacheTime, c,
			func(c *gin.Context) string {
				url := c.Request.URL.String()
				return util.GetMd5([]byte(url))
			},
			DefaultApiRespShouldCacheHandler)
	}
}

func cachePostByMemcache(cacheTime int32, m *CacheMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.Body == nil {
			return
		}
		cacheRequestByMemcache(
			m, cacheTime, c,
			func(c *gin.Context) string {
				bodyBytes, _ := ioutil.ReadAll(c.Request.Body)
				urlBytes := []byte(c.Request.URL.String())
				return util.GetMd5(append(bodyBytes, urlBytes...))
			},
			DefaultApiRespShouldCacheHandler)
	}
}

func cacheGetByRedis(cacheTime int32, m *CacheMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			return
		}

		cacheRequestByRedis(
			m, cacheTime, c,
			func(c *gin.Context) string {
				url := c.Request.URL.String()
				//common.GetMd5([]byte(url))
				return url
			},
			DefaultApiRespShouldCacheHandler)
	}
}

func cachePostByRedis(cacheTime int32, m *CacheMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.Body == nil {
			return
		}
		cacheRequestByRedis(
			m, cacheTime, c,
			func(c *gin.Context) string {
				bodyBytes, _ := ioutil.ReadAll(c.Request.Body)
				urlBytes := []byte(c.Request.URL.String())
				return string(append(bodyBytes, urlBytes...))
			},
			DefaultApiRespShouldCacheHandler)
	}
}

func DefaultApiRespShouldCacheHandler(apiResp *module.ApiResp) bool {
	// 请求成功，将 api 结果写入 cache
	return apiResp.Errno == util.SUCCESS_CODE
}

func cacheRequestByMemcache(m *CacheMiddleware, cacheTime int32, c *gin.Context, keyGetter cacheKeyGetter, shouldCache shouldCacheHandler) {
	if !m.enableCache {
		return
	}
	//start := time.Now()
	//请求key
	key := keyGetter(c)
	//锁定key
	isLockKey := util.GetMd5([]byte(LOCK + key))
	//请求缓存时间
	isLockTimeKey := util.GetMd5([]byte(REQUEST_CACHE_TIME + key))
	//缓存结果
	resp, err := m.cacheClientMemcache.Get(key)

	//是否锁定
	isLock, errCache := m.cacheClientMemcache.Get(isLockKey)
	//锁定时间
	lockTime, _ := m.cacheClientMemcache.Get(isLockTimeKey)

	// cache 中存在对应的条目
	if err == nil {
		//common.Logger.Info("缓存数据::",string(resp))
		var respMap map[string]interface{}
		json.Unmarshal(resp, &respMap)
		c.AbortWithStatusJSON(http.StatusOK, respMap)
		//是否强制更新缓存
		isUpdate := false
		// 缓存时间和缓存有效时间可以找到 并且 没有被锁定
		if string(isLock) == REQUEST_UNLOCK  || errCache == gouache.ErrCacheMiss {
			// 缓存设置的时间 生成时间
			locktimeInt, _ := strconv.Atoi(string(lockTime))
			if locktimeInt > 0  {
				// 如果当前时间 - 缓存设置的时间  >= 缓存时间 我们要强制更新缓存
				if (time.Now().Unix() - int64(locktimeInt)) >= int64(cacheTime) {
					isUpdate = true
				}
			}else{
				isUpdate = true
			}
			log.Printf("有缓存,强制更新缓存%s----%v----%t:", c.Request.RequestURI, locktimeInt, isUpdate)
		}
		if !isUpdate {
			return
		}
	}

	// 锁定了并且没有缓存 直接返回空
	if string(isLock) == REQUEST_LOCK {
		//直接返回空
		c.AbortWithStatusJSON(http.StatusOK, module.ApiResp{
			Errno:  util.SUCCESS_CODE,
			Errmsg: "Try again later",
			Data:   []interface{}{},
		})
		return
	}
	// 没有锁定 锁定相同的请求
	if errCache == gouache.ErrCacheMiss || string(isLock) == REQUEST_UNLOCK {
		//锁定
		if err := m.cacheClientMemcache.Set(isLockKey, []byte(REQUEST_LOCK), 600); err != nil {
			log.Println("锁定err：", isLockKey, isLock, err, cacheTime)
		}

	}

	// cache 中没有对应的条目，继续后续执行
	blw := &bufferedWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
	c.Writer = blw
	c.Next()
	statusCode := c.Writer.Status()
	// 不缓存失败的请求
	if statusCode != http.StatusOK {
		return
	}

	if blw.body.String() == "" {
		return
	}

	// 获取 api 执行结果
	var apiResp module.ApiResp
	body := blw.body.Bytes()
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return
	}

	if shouldCache(&apiResp) {
		// 缓存结果 不过期
		if err := m.cacheClientMemcache.Set(key, body, 0); err != nil {
			log.Println("缓存接口结果失败err：", isLockKey, isLock, err)
		}
		// 缓存返回结果的时间和接口执行的时间
		if err := m.cacheClientMemcache.Set(isLockTimeKey, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0); err != nil {
			log.Println("缓存锁定时间和缓存时间失败err：", isLockKey, isLock, err)
		}

		//解锁
		if err := m.cacheClientMemcache.Set(isLockKey, []byte(REQUEST_UNLOCK), 600); err != nil {
			log.Println("解锁err：", isLockKey, isLock, err, cacheTime)
		}
	}
}

func cacheRequestByRedis(m *CacheMiddleware, cacheTime int32, c *gin.Context, keyGetter cacheKeyGetter, shouldCache shouldCacheHandler) {
	if !m.enableCache {
		return
	}
	//请求key
	key := keyGetter(c)
	//锁定key
	isLockKey := LOCK + key
	//锁定时间
	isLockTimeKey := REQUEST_CACHE_TIME + key
	//缓存结果
	resp, err := m.cacheClientRedis.Client.Get(key).Result()

	//是否锁定
	isLock, errCache := m.cacheClientRedis.Client.Get(isLockKey).Result()
	//锁定时间
	lockTime, errCacheLockTime := m.cacheClientRedis.Client.Get(isLockTimeKey).Result()

	log.Println("测试：",isLock, errCache,lockTime, errCacheLockTime,err)
	// cache 中存在对应的条目
	if err == nil {
		//是否强制更新缓存
		isUpdate := false
		exists := m.cacheClientRedis.Client.Exists(isLockKey).Val()
		// 缓存时间和缓存有效时间可以找到 并且 没有被锁定
		if isLock == REQUEST_UNLOCK || exists == 0 {
			// 缓存设置的时间 生成时间
			locktimeInt, err := strconv.Atoi(lockTime)
			if err != nil {
				locktimeInt = 0
			}
			if locktimeInt > 0  {
				// 如果当前时间 - 缓存设置的时间  >= 缓存时间 我们要强制更新缓存
				if (time.Now().Unix() - int64(locktimeInt)) >= int64(cacheTime) {
					isUpdate = true
				}
			}else{
				isUpdate = true
			}
			log.Println("有缓存,强制更新缓存%s----%v----%t:", c.Request.RequestURI, locktimeInt, isUpdate)
		}
		if !isUpdate {
			var respMap map[string]interface{}
			json.Unmarshal([]byte(resp), &respMap)
			c.AbortWithStatusJSON(http.StatusOK, respMap)
			return
		}
	}

	// 锁定了并且没有缓存 直接返回空
	if isLock == REQUEST_LOCK {
		//直接返回空
		c.AbortWithStatusJSON(http.StatusOK, module.ApiResp{
			Errno:  util.SUCCESS_CODE,
			Errmsg: "Try again later",
			Data:   []interface{}{},
		})
		return
	}
	exists := m.cacheClientRedis.Client.Exists(isLockKey).Val()
	// 没有锁定 锁定相同的请求
	if exists == 0 || isLock == REQUEST_UNLOCK {
		//锁定
		if err := m.cacheClientRedis.Client.Set(isLockKey, REQUEST_LOCK, 600*time.Second).Err(); err != nil {
			log.Println("锁定err：", isLockKey, isLock, err, cacheTime)
		}
	}

	// cache 中没有对应的条目，继续后续执行
	blw := &bufferedWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
	c.Writer = blw
	c.Next()
	statusCode := c.Writer.Status()
	// 不缓存失败的请求
	if statusCode != http.StatusOK {
		return
	}

	if blw.body.String() == "" {
		return
	}

	// 获取 api 执行结果
	var apiResp module.ApiResp
	body := blw.body.Bytes()
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return
	}

	if shouldCache(&apiResp) {
		// 缓存结果
		if err := m.cacheClientRedis.Client.Set(key, string(body), 0).Err(); err != nil {
			log.Println("缓存接口结果失败err：", isLockKey, isLock, err)
		}

		// 缓存返回结果的时间和接口执行的时间
		if err := m.cacheClientRedis.Client.Set(isLockTimeKey, fmt.Sprintf("%d", time.Now().Unix()), 0).Err(); err != nil {
			log.Println("缓存锁定时间和缓存时间失败err：", isLockKey, isLock, err)
		}

		//解锁
		if err := m.cacheClientRedis.Client.Set(isLockKey, REQUEST_UNLOCK, 600*time.Second).Err(); err != nil {
			log.Println("解锁失败err：", isLockKey, isLock, err, cacheTime)
		}

	}
}
