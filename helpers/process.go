package helpers

import (
	"context"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"os"
	"strconv"
)

func GetCurrentProcessNumber() int {
	c := os.Getenv("COUNT")
	count, _ := strconv.Atoi(c)
	return count
}

func GetProcessCount() int {
	val, _ := core.Base.GetRedis("cache").Get(context.Background(), "schedulers").Result()
	count, _ := strconv.Atoi(val)
	return count - 1
}
