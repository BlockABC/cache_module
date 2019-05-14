package cache

import (
	"bytes"
	"encoding/json"
	"github.com/BlockABC/cache_module/memche"
	"github.com/BlockABC/cache_module/module"
	"github.com/BlockABC/cache_module/redis"
	"github.com/BlockABC/cache_module/util"
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
	RequestUnlock = "0"
	RequestLock   = "1"

	MemCache         = 1
	Redis            = 2
	LOCK             = "lock:"
	RequestCacheTime = "request_cache_time:"
)

type cacheKeyGetter = func(c *gin.Context) string
type shouldCacheHandler = func(apiResp *module.ApiResp) bool

type Middleware struct {
	cacheClientMemCache *memche.Client
	cacheClientRedis    *redis.Client
	enableCache         bool
}

func NewCacheMiddleware(cacheClientMemCache *memche.Client, cacheClientRedis *redis.Client, enableCache bool) *Middleware {
	middleware := Middleware{
		cacheClientMemCache: cacheClientMemCache,
		cacheClientRedis:    cacheClientRedis,
		enableCache:         enableCache,
	}
	return &middleware
}

func (m *Middleware) CacheGET(cacheTime, cacheType int32) gin.HandlerFunc {
	switch cacheType {
	case MemCache:
		return cacheGetByMemCache(cacheTime, m)
	case Redis:
		return cacheGetByRedis(cacheTime, m)
	default:
		return cachePostByRedis(cacheTime, m)
	}
}

func (m *Middleware) CachePOST(cacheTime, cacheType int32) gin.HandlerFunc {
	switch cacheType {
	case MemCache:
		return cachePostByMemCache(cacheTime, m)
	case Redis:
		return cachePostByRedis(cacheTime, m)
	default:
		return cachePostByRedis(cacheTime, m)
	}
}

func cacheGetByMemCache(cacheTime int32, m *Middleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			return
		}

		cacheRequestByMemCache(
			m, cacheTime, c,
			func(c *gin.Context) string {
				url := c.Request.URL.String()
				return util.GetMd5([]byte(url))
			},
			DefaultApiRespShouldCacheHandler)
	}
}

func cachePostByMemCache(cacheTime int32, m *Middleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.Body == nil {
			return
		}
		cacheRequestByMemCache(
			m, cacheTime, c,
			func(c *gin.Context) string {
				bodyBytes, _ := ioutil.ReadAll(c.Request.Body)
				urlBytes := []byte(c.Request.URL.String())
				return util.GetMd5(append(bodyBytes, urlBytes...))
			},
			DefaultApiRespShouldCacheHandler)
	}
}

func cacheGetByRedis(cacheTime int32, m *Middleware) gin.HandlerFunc {
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

func cachePostByRedis(cacheTime int32, m *Middleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.Body == nil {
			return
		}
		cacheRequestByRedis(
			m, cacheTime, c,
			func(c *gin.Context) string {
				//bodyBytes, _ := ioutil.ReadAll(c.Request.Body)
				bodyBytes, _ := c.GetRawData()
				urlBytes := []byte(c.Request.URL.String())
				// gin post 参数只能读取一次 所以需要把body传下去
				c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes)) // 关键点
				return string(append(urlBytes, bodyBytes...))
			},
			DefaultApiRespShouldCacheHandler)
	}
}

func DefaultApiRespShouldCacheHandler(apiResp *module.ApiResp) bool {
	// 请求成功，将 api 结果写入 cache
	return apiResp.Errno == util.SUCCESS_CODE
}

func cacheRequestByMemCache(m *Middleware, cacheTime int32, c *gin.Context, keyGetter cacheKeyGetter, shouldCache shouldCacheHandler) {
	if !m.enableCache {
		return
	}
	//请求key
	key := keyGetter(c)
	//锁定key
	isLockKey := util.GetMd5([]byte(LOCK + key))
	//请求缓存时间
	isLockTimeKey := util.GetMd5([]byte(RequestCacheTime + key))
	//缓存结果
	resp, err := m.cacheClientMemCache.Get(key)
	//是否锁定
	isLock, errCache := m.cacheClientMemCache.Get(isLockKey)
	//锁定时间
	lockTime, _ := m.cacheClientMemCache.Get(isLockTimeKey)
	// cache 中存在对应的条目
	if err == nil {
		var respMap map[string]interface{}
		json.Unmarshal(resp, &respMap)
		c.AbortWithStatusJSON(http.StatusOK, respMap)
		//是否强制更新缓存
		isUpdate := false
		// 缓存时间和缓存有效时间可以找到 并且 没有被锁定
		if string(isLock) == RequestUnlock || errCache == gouache.ErrCacheMiss {
			// 缓存设置的时间 生成时间
			lockTimeInt, _ := strconv.Atoi(string(lockTime))
			if lockTimeInt > 0 {
				// 如果当前时间 - 缓存设置的时间  >= 缓存时间 我们要强制更新缓存
				if (time.Now().Unix() - int64(lockTimeInt)) >= int64(cacheTime) {
					isUpdate = true
				}
			} else {
				isUpdate = true
			}
			log.Printf("There is a cache, forcing updates to the cache %s----%v----%t:", c.Request.RequestURI, lockTimeInt, isUpdate)
		}
		if !isUpdate {
			return
		}
	}

	// 锁定了并且没有缓存 直接返回空
	if string(isLock) == RequestLock {
		//直接返回空
		c.AbortWithStatusJSON(http.StatusOK, module.ApiResp{
			Errno:  util.RequestLock,
			Errmsg: "Try again later",
			Data:   []interface{}{},
		})
		return
	}
	// 没有锁定 锁定相同的请求
	if errCache == gouache.ErrCacheMiss || string(isLock) == RequestUnlock {
		//锁定
		if err := m.cacheClientMemCache.Set(isLockKey, []byte(RequestLock), 600); err != nil {
			log.Println("lock err：", isLockKey, isLock, err, cacheTime)
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
		if err := m.cacheClientMemCache.Set(key, body, 0); err != nil {
			log.Println("The cache interface failed err：", isLockKey, isLock, err)
		}
		// 缓存返回结果的时间和接口执行的时间
		if err := m.cacheClientMemCache.Set(isLockTimeKey, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0); err != nil {
			log.Println("Cache lock time and cache time failed err：", isLockKey, isLock, err)
		}

		//解锁
		if err := m.cacheClientMemCache.Set(isLockKey, []byte(RequestUnlock), 600); err != nil {
			log.Println("Unlock err：", isLockKey, isLock, err, cacheTime)
		}
	}
}

func cacheRequestByRedis(m *Middleware, cacheTime int32, c *gin.Context, keyGetter cacheKeyGetter, shouldCache shouldCacheHandler) {
	if !m.enableCache {
		return
	}
	//请求key
	key := keyGetter(c)
	//锁定key
	isLockKey := LOCK + key
	//锁定时间
	isLockTimeKey := RequestCacheTime + key
	//缓存结果
	resp, err := m.cacheClientRedis.Client.Get(key).Result()
	//是否锁定
	isLock, _ := m.cacheClientRedis.Client.Get(isLockKey).Result()
	//锁定时间
	lockTime, _ := m.cacheClientRedis.Client.Get(isLockTimeKey).Result()
	//log.Print("test：", isLock, errCache, lockTime, errCacheLockTime, err)
	// cache 中存在对应的条目
	if err == nil {
		//是否强制更新缓存
		isUpdate := false
		exists := m.cacheClientRedis.Client.Exists(isLockKey).Val()
		// 缓存时间和缓存有效时间可以找到 并且 没有被锁定
		if isLock == RequestUnlock || exists == 0 {
			// 缓存设置的时间 生成时间
			lockTimeInt, err := strconv.Atoi(lockTime)
			if err != nil {
				lockTimeInt = 0
			}
			if lockTimeInt > 0 {
				// 如果当前时间 - 缓存设置的时间  >= 缓存时间 我们要强制更新缓存
				if (time.Now().Unix() - int64(lockTimeInt)) >= int64(cacheTime) {
					isUpdate = true
				}
			} else {
				isUpdate = true
			}
			log.Printf("There is a cache, forcing updates to the cache%s----%v----%t:", c.Request.RequestURI, lockTimeInt, isUpdate)
		}
		if !isUpdate {
			var respMap map[string]interface{}
			json.Unmarshal([]byte(resp), &respMap)
			c.AbortWithStatusJSON(http.StatusOK, respMap)
			return
		}
	}

	// 锁定了并且没有缓存 直接返回空
	if isLock == RequestLock {
		//直接返回空
		c.AbortWithStatusJSON(http.StatusOK, module.ApiResp{
			Errno:  util.RequestLock,
			Errmsg: "Try again later",
			Data:   []interface{}{},
		})
		return
	}
	exists := m.cacheClientRedis.Client.Exists(isLockKey).Val()
	// 没有锁定 锁定相同的请求
	if exists == 0 || isLock == RequestUnlock {
		//锁定
		if err := m.cacheClientRedis.Client.Set(isLockKey, RequestLock, 600*time.Second).Err(); err != nil {
			log.Println("lock err：", isLockKey, isLock, err, cacheTime)
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
		//有返回解锁
		if err := m.cacheClientRedis.Client.Set(isLockKey, RequestUnlock, 600*time.Second).Err(); err != nil {
			log.Println("Unlock err：", isLockKey, isLock, err, cacheTime)
		}
		return
	}

	if shouldCache(&apiResp) {
		// 缓存结果
		if err := m.cacheClientRedis.Client.Set(key, string(body), 0).Err(); err != nil {
			log.Println("The cache interface failed err：", isLockKey, isLock, err)
		}

		// 缓存返回结果的时间和接口执行的时间
		if err := m.cacheClientRedis.Client.Set(isLockTimeKey, fmt.Sprintf("%d", time.Now().Unix()), 0).Err(); err != nil {
			log.Println("Cache lock time and cache time failed err：", isLockKey, isLock, err)
		}
	}
	//有返回解锁
	if err := m.cacheClientRedis.Client.Set(isLockKey, RequestUnlock, 600*time.Second).Err(); err != nil {
		log.Println("Unlock err：", isLockKey, isLock, err, cacheTime)
	}

	defer func() {
		if r := recover(); r != nil {
			//有返回解锁
			if err := m.cacheClientRedis.Client.Set(isLockKey, RequestUnlock, 600*time.Second).Err(); err != nil {
				log.Println("Unlock err：", isLockKey, isLock, err, cacheTime)
			}
		}
	}()
}
