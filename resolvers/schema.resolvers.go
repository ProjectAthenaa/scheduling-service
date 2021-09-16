package resolvers

// This file will be automatically regenerated based on the schema, any resolver implementations
// will be copied through when generating and any unknown code will be moved to the end.

import (
	"context"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/task"
	"sync"
	"time"

	"github.com/ProjectAthenaa/scheduling-service/graph/generated"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/scheduling-service/scheduler"
	"github.com/ProjectAthenaa/sonic-core/protos/module"
	"github.com/ProjectAthenaa/sonic-core/sonic"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	user2 "github.com/ProjectAthenaa/sonic-core/sonic/database/ent/user"
)

func (r *mutationResolver) SendCommand(ctx context.Context, controlToken string, command model.Command) (bool, error) {
	if _, err := contextExtract(ctx); err != nil {
		return false, err
	}
	if err := scheduler.PublishCommand(ctx, controlToken, command); err != nil {
		return false, err
	}
	return true, nil
}

func (r *mutationResolver) StartTask(ctx context.Context, taskIDs []string) (bool, error) {
	userID, err := contextExtract(ctx)
	if err != nil {
		return false, err
	}

	user, err := core.Base.GetPg("pg").User.Query().WithApp().Where(user2.ID(sonic.UUIDParser(*userID))).First(ctx)
	if err != nil {
		return false, sonic.EntErr(err)
	}

	var wg sync.WaitGroup

	for _, id := range taskIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tsk, err := user.Edges.App.QueryTaskGroups().QueryTasks().Where(task.ID(sonic.UUIDParser(id))).First(ctx)
			if err != nil {
				return
			}

			if _, err = tsk.Update().SetStartTime(time.Now().Add(time.Second)).Save(ctx); err != nil {
				return
			}
		}()

	}
	wg.Wait()

	return true, nil
}

func (r *queryResolver) GetScheduledTasks(ctx context.Context) ([]*model.Task, error) {
	userID, err := contextExtract(ctx)
	if err != nil {
		return nil, err
	}
	return scheduler.GetUserTasks(*userID), nil
}

func (r *subscriptionResolver) TaskUpdates(ctx context.Context, subscriptionTokens []string) (<-chan *model.TaskStatus, error) {
	var updateIDs []string

	updates := make(chan *model.TaskStatus)
	pubSub, closePubSub, err := scheduler.Subscribe(ctx, subscriptionTokens...)
	if err != nil {
		return nil, err
	}
	go func() {
		var status module.Status
		defer pubSub.Close()
		defer closePubSub()
	nextUpdate:
		for update := range pubSub.Channel() {
			if err = json.Unmarshal([]byte(update.Payload), &status); err != nil {
				updates <- &model.TaskStatus{
					Status: model.StatusError,
					Error:  sonic.ErrString(err),
				}
				continue
			}

			if status.Status == module.STATUS_STOPPED {
				continue nextUpdate
			}

			for _, id := range updateIDs {
				if id == status.Information["id"] {
					continue nextUpdate
				}
			}

			updateIDs = append(updateIDs, status.Information["id"])

			returningStatus := &model.TaskStatus{
				TaskID:      status.Information["taskID"],
				Status:      model.Status(module.STATUS_name[int32(status.Status)]),
				Error:       status.Error,
				Information: map[string]interface{}{},
			}

			for k, v := range status.Information {
				if k == "taskID" {
					continue
				}
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
