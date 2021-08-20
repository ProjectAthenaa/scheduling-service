package main

import (
	"context"
	"fmt"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/ProjectAthenaa/scheduling-service/graph/generated"
	"github.com/ProjectAthenaa/scheduling-service/resolvers"
	"github.com/ProjectAthenaa/scheduling-service/scheduler"
	"github.com/ProjectAthenaa/sonic-core/authentication"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/gin-gonic/gin"
	"log"
	"os"
	"os/signal"
	"syscall"
)

const defaultPort = "8080"

func init() {
	go func() {
		if os.Getenv("DEBUG") == "1" {
			core.Base.GetRedis("cache").Del(context.Background(), "schedulers")
		}

		count, _ := core.Base.GetRedis("cache").Incr(context.Background(), "schedulers").Result()
		os.Setenv("COUNTER", fmt.Sprint(count-1))
		c := make(chan os.Signal, 1)
		defer close(c)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		scheduler.Stop()
		core.Base.GetRedis("cache").Decr(context.Background(), "schedulers")
	}()
}

// Defining the Graphql handler
func graphqlHandler() gin.HandlerFunc {
	// NewExecutableSchema and Config are in the generated.go file
	// Resolver is in the resolver.go file
	h := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: &resolvers.Resolver{}}))

	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// Defining the Playground handler
func playgroundHandler() gin.HandlerFunc {
	h := playground.Handler("GraphQL", "/query")

	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

func main() {
	r := gin.Default()

	r.Use(authentication.GenGraphQLAuthenticationFunc(core.Base, nil)())

	r.POST("/query", graphqlHandler())
	r.GET("/query", graphqlHandler())
	r.GET("/", playgroundHandler())
	if err := r.Run(); err != nil{
		log.Fatal(err)
	}
}
