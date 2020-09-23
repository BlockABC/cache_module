package cache

import (
	"bytes"
	"encoding/json"
	"github.com/BlockABC/cache_module/memche"
	"github.com/BlockABC/cache_module/module"
	"github.com/BlockABC/cache_module/util"
	"github.com/go-redis/redis"
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
	Salt             = "salt"
	RequestUnlock    = "0"
	RequestLock      = "1"
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

func NewCacheMiddleware(memCache *memche.Client, redisCache *redis.Client, enableCache bool) *Middleware {
	middleware := Middleware{
		cacheClientMemCache: memCache,
		cacheClientRedis:    redisCache,
		enableCache:         enableCache,
	}
	return &middleware
}

/*
** cacheTime:接口过期时间，过期后会重新从DB拿数据并更新到缓存
** redisTime:redis内容过期时间，避免redis溢出问题
 */
func (m *Middleware) CacheGet(cacheTime, cacheType int32, redisTime time.Duration) gin.HandlerFunc {
	switch cacheType {
	case MemCache:
		return cacheGetByMemCache(cacheTime, m)
	case Redis:
		return cacheGetByRedis(m, cacheTime, redisTime)
	default:
		return cachePostByRedis(m, cacheTime, redisTime)
	}
}

/*
** cacheTime:接口过期时间，过期后会重新从DB拿数据并更新到缓存
** redisTime:redis内容过期时间，避免redis溢出问题
 */
func (m *Middleware) CachePost(cacheTime, cacheType int32, redisTime time.Duration) gin.HandlerFunc {
	switch cacheType {
	case MemCache:
		return cachePostByMemCache(cacheTime, m)
	case Redis:
		return cachePostByRedis(m, cacheTime, redisTime)
	default:
		return cachePostByRedis(m, cacheTime, redisTime)
	}
}

func cacheGetByMemCache(cacheTime int32, m *Middleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			return
		}
		cacheRequestByMemCache(m, cacheTime, c,
			func(c *gin.Context) string {
				url := c.Request.URL.String()
				return util.GetMd5([]byte(url))
			}, DefaultApiRespShouldCacheHandler)
	}
}

func cachePostByMemCache(cacheTime int32, m *Middleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.Body == nil {
			return
		}
		cacheRequestByMemCache(m, cacheTime, c,
			func(c *gin.Context) string {
				bodyBytes, _ := ioutil.ReadAll(c.Request.Body)
				urlBytes := []byte(c.Request.URL.String())
				return util.GetMd5(append(bodyBytes, urlBytes...))
			}, DefaultApiRespShouldCacheHandler)
	}
}

func cacheGetByRedis(m *Middleware, cacheTime int32, redisTime time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			return
		}
		cacheRequestByRedis(m, redisTime, cacheTime, c,
			func(c *gin.Context) string {
				cook, _ := json.Marshal(c.Request.Cookies()) //加入cookie的部分
				url := c.Request.URL.String() + c.GetString(Salt) + string(cook)
				return url
			}, DefaultApiRespShouldCacheHandler)
	}
}

func cachePostByRedis(m *Middleware, cacheTime int32, redisTime time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.Body == nil {
			return
		}
		cacheRequestByRedis(m, redisTime, cacheTime, c,
			func(c *gin.Context) string {
				bodyBytes, _ := c.GetRawData()
				cook, _ := json.Marshal(c.Request.Cookies())
				urlBytes := append([]byte(c.Request.URL.String()), cook...)
				c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes)) // 关键点
				return string(append(urlBytes, bodyBytes...))
			}, DefaultApiRespShouldCacheHandler)
	}
}

func DefaultApiRespShouldCacheHandler(apiResp *module.ApiResp) bool {
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
		_ = json.Unmarshal(resp, &respMap)
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

/*
** redis缓存接口，为了避免同一请求在无缓存时同时下放到DB，需要先锁定接口，再请求数据，在请求返回之前，同样的请求只能拉取到上一次缓存的数据，同时设置一个redis过期时间，避免缓存溢出问题
 */
func cacheRequestByRedis(m *Middleware, redisTime time.Duration, cacheTime int32, c *gin.Context, keyGetter cacheKeyGetter, shouldCache shouldCacheHandler) {
	if !m.enableCache {
		return
	}
	/*如果程序异常挂掉，需要清空lock，否则进入此接口就会一直走锁定逻辑，
	这里的问题点是每十分钟会有一次自动解锁，可能会造成新的请求下放到DB，
	之所以不设置更短时间，是为了防止接口迟迟不返回导致请求下放到DB的问题，TIDB经常会因为这个挂掉*/
	lockTime := 10 * time.Minute
	key := keyGetter(c)                     //请求key
	lockKey := LOCK + key                   //锁定key
	updateTimeKey := RequestCacheTime + key //接口更新时间
	defer m.cacheClientRedis.Set(lockKey, RequestUnlock, lockTime)
	defer func() { //发生异常时，解除锁定，便于定位问题
		if r := recover(); r != nil {
			if err := m.cacheClientRedis.Set(lockKey, RequestUnlock, lockTime).Err(); err != nil {
				logger.Println("Unlock err：", lockKey, err, cacheTime)
			} //有返回解锁
		}
	}()
	isLock, _ := m.cacheClientRedis.Get(lockKey).Result() //是否锁定，当缓存为空，isLock将返回空，err可以忽略
	if isLock == RequestUnlock {
		_ = m.cacheClientRedis.Set(lockKey, RequestLock, lockTime).Err() //立即锁定接口，保证只能有一个相同请求进入
	}
	if isLock == RequestLock { //锁定，当接口锁定，即这个接口已被进入，此时相同请求不能透传到数据库，如果缓存有数据，走缓存，缓存无数据返回空
		if resp, err := m.cacheClientRedis.Get(key).Result(); err == nil {
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
	lastTime, _ := m.cacheClientRedis.Get(updateTimeKey).Int64() //更新时间，如果出错，lockTime为0，肯定会超出缓存时间，因此可以不检查
	if (time.Now().Unix() - lastTime) <= int64(cacheTime) {      //缓存还未过期，走缓存
		if resp, err := m.cacheClientRedis.Get(key).Result(); err == nil {
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
		if err := m.cacheClientRedis.Set(key, string(body), redisTime).Err(); err != nil {
			logger.Println("The cache interface failed err：", lockKey, isLock, err)
			return
		}
		// 更新缓存中返回结果的时间和接口执行的时间
		if err := m.cacheClientRedis.Set(updateTimeKey, time.Now().Unix(), time.Hour*48).Err(); err != nil {
			log.Println("Cache lock time and cache time failed err：", lockKey, isLock, err)
			return
		}
	}
}
