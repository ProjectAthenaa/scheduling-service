package main

import (
	"context"
	"fmt"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/ProjectAthenaa/scheduling-service/graph/generated"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/scheduling-service/resolvers"
	"github.com/ProjectAthenaa/scheduling-service/scheduler"
	"github.com/ProjectAthenaa/sonic-core/authentication"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"log"
	"os"
	"os/signal"
	"strconv"
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

	var sampleRate float64

	sR := os.Getenv("SAMPLE_RATE")
	if len(sR) != 0 {
		sampleRate, _ = strconv.ParseFloat(sR, 64)
	}

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              "https://73eb034025e6462b961137b5b93c6265@o706779.ingest.sentry.io/5951247",
		ServerName:       "Integration Service",
		Environment:      os.Getenv("ENVIRONMENT"),
		TracesSampleRate: sampleRate,
	}); err != nil {
		log.Fatalln("sentry.Init: ", err)
	}

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
	h := playground.Handler("GraphQL", "/tasks")

	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

func main() {
	r := gin.Default()

	if os.Getenv("DEBUG") == "1" {
		fmt.Println("Current Scheduler Index: ", helpers.GetCurrentProcessNumber())
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r.Use(authentication.GenGraphQLAuthenticationFunc(core.Base, "/tasks", nil)())

	r.Any("/tasks", graphqlHandler())
	r.GET("/tasks/playground", playgroundHandler())
	if err := r.Run(); err != nil {
		log.Fatal(err)
	}
}
