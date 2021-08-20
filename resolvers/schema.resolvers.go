package resolvers

// This file will be automatically regenerated based on the schema, any resolver implementations
// will be copied through when generating and any unknown code will be moved to the end.

import (
	"context"
	"github.com/ProjectAthenaa/scheduling-service/graph/generated"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/scheduling-service/scheduler"
	module "github.com/ProjectAthenaa/sonic-core/protos"
	"github.com/ProjectAthenaa/sonic-core/sonic"
	tasks "github.com/ProjectAthenaa/sonic-core/task_controller"
)

//SendCommand is a short lifetime function which accepts a control token and a command, it then calls the PublishCommand
//method of the scheduler package to publish the command to redis
func (r *mutationResolver) SendCommand(ctx context.Context, controlToken string, command model.Command) (bool, error) {
	if _, err := contextExtract(ctx); err != nil {
		return false, err
	}
	if err := scheduler.PublishCommand(ctx, controlToken, command); err != nil {
		return false, err
	}
	return true, nil
}

//GetScheduledTasks returns all tasks that are set to start in the next 30 minutes or are already running
func (r *queryResolver) GetScheduledTasks(ctx context.Context) ([]*model.Task, error) {
	userID, err := contextExtract(ctx)
	if err != nil {
		return nil, err
	}
	return scheduler.GetUserTasks(*userID), nil
}

//TaskUpdates returns a channel in which task updates from redis are piped to, you need a valid subscriptionToken to utilize it
func (r *subscriptionResolver) TaskUpdates(ctx context.Context, subscriptionToken string) (<-chan *model.TaskStatus, error) {
	if _, err := contextExtract(ctx); err != nil {
		return nil, err
	}
	updates := make(chan *model.TaskStatus)
	pubSub, err := scheduler.Subscribe(ctx, subscriptionToken)
	if err != nil {
		return nil, err
	}
	go func() {
		var status tasks.Status
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
				Information: map[string]interface{}{},
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
