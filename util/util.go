package util

import (
	"crypto/md5"
	"encoding/hex"
)

const (
	SUCCESS_CODE = 0
)

func GetMd5(message []byte) (tmp string) {

	md5Ctx := md5.New()
	md5Ctx.Write(message)
	cipherStr := md5Ctx.Sum(nil)
	tmp = hex.EncodeToString(cipherStr)
	return tmp
}
