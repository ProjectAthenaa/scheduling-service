package scheduler

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/go-redis/redis/v8"
)

var scheduler = NewScheduler()

func init() {
	if err := populateMap(); err != nil {
		panic(err)
	}
	scheduler.init()
}

//GetUserTasks retrieves user scheduled tasks
func GetUserTasks(userID string) []*model.Task {
	return scheduler.getUserTasks(userID)
}

//Stop stops the scheduler
func Stop() {
	scheduler.cancelFunc()
}

//Subscribe returns a redis pubSub struct if the subscription token is valid
func Subscribe(ctx context.Context, tokens ...string) (*redis.PubSub, func() error, error) {
	var channelNames []string
	scheduler.locker.Lock()
	defer scheduler.locker.Unlock()

	for _, token := range tokens {
		channelNames = append(channelNames, fmt.Sprintf("tasks:updates:%s", token))
	}

	pubsub := core.Base.GetRedis("cache").Subscribe(ctx, channelNames...)

	closePubSub := func() error {
		if err := pubsub.Unsubscribe(ctx, channelNames...); err != nil {
			return err
		}
		return nil
	}

	return core.Base.GetRedis("cache").Subscribe(ctx, channelNames...), closePubSub, nil
}

//PublishCommand publishes the given command to the channel given, if the task exists
func PublishCommand(ctx context.Context, token string, command model.Command) error {
	scheduler.locker.Lock()
	defer scheduler.locker.Unlock()
	core.Base.GetRedis("cache").Publish(ctx, fmt.Sprintf("tasks:commands:%s", token), command)
	return nil
}
