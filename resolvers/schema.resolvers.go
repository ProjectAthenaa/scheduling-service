package resolvers

// This file will be automatically regenerated based on the schema, any resolver implementations
// will be copied through when generating and any unknown code will be moved to the end.

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/graph/generated"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/scheduling-service/scheduler"
	module "github.com/ProjectAthenaa/sonic-core/protos"
	"github.com/ProjectAthenaa/sonic-core/sonic"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	tasks "github.com/ProjectAthenaa/sonic-core/task_controller"
)

func (r *mutationResolver) SendCommand(ctx context.Context, controlToken string, command model.Command) (bool, error) {
	core.Base.GetRedis("cache").Publish(ctx, fmt.Sprintf("tasks:command:%s", controlToken), command)
	return true, nil
}

func (r *queryResolver) GetScheduledTasks(ctx context.Context) ([]*model.Task, error) {
	userID := getUserIDFromContext(ctx)
	return scheduler.GetUserTasks(userID), nil
}

func (r *subscriptionResolver) TaskUpdates(ctx context.Context, subscriptionToken string) (<-chan *model.TaskStatus, error) {
	updates := make(chan *model.TaskStatus)
	pubSub := core.Base.GetRedis("cache").Subscribe(ctx, fmt.Sprintf("tasks:updates:%s", subscriptionToken))

	go func() {
		var status tasks.Status
		var err error
		defer pubSub.Close()
		for update := range pubSub.Channel() {
			if err = json.Unmarshal([]byte(update.Payload), &status); err != nil {
				updates <- &model.TaskStatus{
					Status: model.StatusError,
					Error:  sonic.ErrString(err),
				}

				continue
			}

			returningStatus := &model.TaskStatus{
				Status:      model.Status(module.STATUS_name[int32(status.Status)]),
				Error:       status.Error,
				Information: nil,
			}

			for k, v := range status.Information {
				returningStatus.Information[k] = v
			}

			updates <- returningStatus
		}
	}()

	return updates, nil
}

// Mutation returns generated.MutationResolver implementation.
func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{r} }

// Subscription returns generated.SubscriptionResolver implementation.
func (r *Resolver) Subscription() generated.SubscriptionResolver { return &subscriptionResolver{r} }

type mutationResolver struct{ *Resolver }
type queryResolver struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }
