package main

import (
	"context"
	"fmt"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/ProjectAthenaa/scheduling-service/graph/generated"
	"github.com/ProjectAthenaa/scheduling-service/resolvers"
	"github.com/ProjectAthenaa/scheduling-service/scheduler"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

const defaultPort = "8080"

func init() {
	go func() {
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

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	srv := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: &resolvers.Resolver{}}))

	http.Handle("/", playground.Handler("GraphQL playground", "/query"))
	http.Handle("/query", srv)

	log.Printf("connect to http://localhost:%s/ for GraphQL playground", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
