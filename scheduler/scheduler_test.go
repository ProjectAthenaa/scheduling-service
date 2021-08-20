package scheduler

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/prometheus/common/log"
	"os"
	"testing"
	"time"
)

func init()  {
	if os.Getenv("DEBUG") == "1"{
		core.Base.GetRedis("cache").Del(context.Background(), "schedulers")
	}

	count, _ := core.Base.GetRedis("cache").Incr(context.Background(), "schedulers").Result()
	os.Setenv("COUNTER", fmt.Sprint(count-1))
}

func exit()  {
	core.Base.GetRedis("cache").Decr(context.Background(), "schedulers")
}

func TestScheduler(t *testing.T) {
	defer exit()
	time.Sleep(time.Second * 5)

	for k := range scheduler.getData(){
		log.Info(k)
	}


}
