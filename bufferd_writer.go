package cache

import (
	"bytes"

	"github.com/gin-gonic/gin"
)

type bufferedWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bufferedWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}
