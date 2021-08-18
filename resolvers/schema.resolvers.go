package resolvers

// This file will be automatically regenerated based on the schema, any resolver implementations
// will be copied through when generating and any unknown code will be moved to the end.

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/scheduler"

	"github.com/ProjectAthenaa/scheduling-service/graph/generated"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
)

func (r *mutationResolver) SendCommand(ctx context.Context, controlToken string, command model.Command) (bool, error) {
	panic(fmt.Errorf("not implemented"))
}

func (r *queryResolver) GetScheduledTasks(ctx context.Context) ([]*model.Task, error) {
	userID := getUserIDFromContext(ctx)
	return scheduler.GetUserTasks(userID), nil
}

func (r *subscriptionResolver) TaskUpdates(ctx context.Context, subscriptionToken string) (<-chan *model.TaskStatus, error) {
	panic(fmt.Errorf("not implemented"))
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
