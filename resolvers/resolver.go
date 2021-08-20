package resolvers

import (
	"context"
	"errors"
	jsoniter "github.com/json-iterator/go"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

type Resolver struct{}

var json = jsoniter.ConfigFastest

func contextExtract(ctx context.Context) (*string, error) {
	if err := ctx.Value("error"); err != nil {
		return nil, err.(error)
	}

	if userID := ctx.Value("userID"); userID != nil {
		id := userID.(string)
		return &id, nil
	}

	return nil, errors.New("user_not_found")
}
