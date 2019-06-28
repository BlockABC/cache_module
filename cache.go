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
	"os"

	"fmt"
	gouache "github.com/bradfitz/gomemcache/memcache"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

var logger = log.New(os.Stdout, "eth_client: ", log.Lshortfile|log.LstdFlags)

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
	} else {
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
	lockTime := 10 * time.Minute //如果程序异常挂掉，需要清空lock，否则进入此接口就会一直走锁定逻辑
	key := keyGetter(c)                     //请求key
	lockKey := LOCK + key                   //锁定key
	updateTimeKey := RequestCacheTime + key //接口更新时间
	defer m.cacheClientRedis.Client.Set(lockKey, RequestUnlock, lockTime)
	defer func() { //发生异常时，解除锁定，便于定位问题
		if r := recover(); r != nil {
			if err := m.cacheClientRedis.Client.Set(lockKey, RequestUnlock, lockTime).Err(); err != nil {
				logger.Println("Unlock err：", lockKey, err, cacheTime)
			} //有返回解锁
		}
	}()
	isLock, _ := m.cacheClientRedis.Client.Get(lockKey).Result() //是否锁定，当缓存为空，isLock将返回空，err可以忽略
	if isLock == RequestUnlock {
		_ = m.cacheClientRedis.Client.Set(lockKey, RequestLock, lockTime).Err() //立即锁定接口，保证只能有一个相同请求进入
	}
	if isLock == RequestLock { //锁定，当接口锁定，即这个接口已被进入，此时相同请求不能透传到数据库，如果缓存有数据，走缓存，缓存无数据返回空
		if resp, err := m.cacheClientRedis.Client.Get(key).Result(); err == nil {
			var respMap map[string]interface{}
			if err := json.Unmarshal([]byte(resp), &respMap); err == nil {
				c.AbortWithStatusJSON(http.StatusOK, respMap)
				return
			}
		}
		//缓存数据取不到时，返回空
		c.AbortWithStatusJSON(http.StatusOK, module.ApiResp{Errno: util.RequestLock, Errmsg: "Try again later", Data: []interface{}{}})
		return
	}
	//接口没有被锁定，那么执行过期检测，如果过期，更新缓存，没有过期，返回缓存
	lastTime, _ := m.cacheClientRedis.Client.Get(updateTimeKey).Int64() //更新时间，如果出错，lockTime为0，肯定会超出缓存时间，因此可以不检查
	if (time.Now().Unix() - lastTime) <= int64(cacheTime) {             //缓存还未过期，走缓存
		if resp, err := m.cacheClientRedis.Client.Get(key).Result(); err == nil {
			var respMap map[string]interface{}
			if err := json.Unmarshal([]byte(resp), &respMap); err == nil {
				c.AbortWithStatusJSON(http.StatusOK, respMap)
				return
			}
		}
	}
	//接口未锁定，且缓存过期，则下放接口到数据库
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
		if err := m.cacheClientRedis.Client.Set(key, string(body), 0).Err(); err != nil {
			logger.Println("The cache interface failed err：", lockKey, isLock, err)
		}
		// 更新缓存中返回结果的时间和接口执行的时间
		if err := m.cacheClientRedis.Client.Set(updateTimeKey, time.Now().Unix(), 0).Err(); err != nil {
			log.Println("Cache lock time and cache time failed err：", lockKey, isLock, err)
		}
	}
}
