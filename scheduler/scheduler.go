package scheduler

import (
	"context"
	"errors"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/go-redis/redis/v8"
)

var scheduler = NewScheduler()

func init() {
	scheduler.init()
}

//GetUserTasks, retrieves user scheduled tasks
func GetUserTasks(userID string) []*model.Task {
	return scheduler.getUserTasks(userID)
}

//Stop, stops the scheduler
func Stop() {
	scheduler.cancelFunc()
}

//Subscribe returns a redis pubSub struct if the subscription token is valid
func Subscribe(ctx context.Context, token string) (*redis.PubSub, error) {
	scheduler.locker.Lock()
	defer scheduler.locker.Unlock()
	for _, tks := range scheduler.data {
		for _, tk := range tks {
			if tk.subscriptionToken == token {
				return core.Base.GetRedis("cache").Subscribe(ctx, fmt.Sprintf("tasks:updates:%s", token)), nil
			}
		}
	}

	return nil, errors.New("task_not_found")
}

//PublishCommand publishes the given command to the channel given, if the task exists
func PublishCommand(ctx context.Context, token string, command model.Command) error {
	scheduler.locker.Lock()
	defer scheduler.locker.Unlock()
	for _, tks := range scheduler.data {
		for _, tk := range tks {
			if tk.controlToken == token {
				core.Base.GetRedis("cache").Publish(ctx, fmt.Sprintf("tasks:command:%s", token), command)
				return nil
			}
		}
	}

	return errors.New("task_not_found")
}
