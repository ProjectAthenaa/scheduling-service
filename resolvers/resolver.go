package resolvers

import "context"

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

type Resolver struct{}

func getUserIDFromContext(ctx context.Context) string {
	return ctx.Value("user_id").(string)
}