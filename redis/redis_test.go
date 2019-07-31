package redis

import (
	"fmt"
	"testing"
	"time"
)

func TestPing(t *testing.T) {
	r := New("127.0.0.1:6379", "", 0, time.Millisecond*100, time.Millisecond*500)
	r1 := New("eosaprk-web.redis.rds.aliyuncs.com:6379", "", 0, time.Millisecond*100)
	fmt.Println("ret:", IsAlive(r))
	fmt.Println("ret:", IsAlive(r1))
}
