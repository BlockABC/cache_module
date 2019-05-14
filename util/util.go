package util

import (
	"crypto/md5"
	"encoding/hex"
	"time"
)

const (
	SUCCESS_CODE = 0
	RequestLock  = 255
)


var NowFunc = func() time.Time {
	return time.Now()
}

func GetMd5(message []byte) (tmp string) {

	md5Ctx := md5.New()
	md5Ctx.Write(message)
	cipherStr := md5Ctx.Sum(nil)
	tmp = hex.EncodeToString(cipherStr)
	return tmp
}
