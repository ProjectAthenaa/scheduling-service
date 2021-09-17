package scheduler

import (
	"context"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/sonic-core/sonic"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	task2 "github.com/ProjectAthenaa/sonic-core/sonic/database/ent/task"
	"github.com/google/uuid"
	"sync"
)

//removeTask removes a given task from the index provided
func removeTask(t []*Task, i int) []*Task {
	t[i] = t[len(t)-1]
	return t[:len(t)-1]
}

//removeID removes a given task from the index provided
func removeID(t []uuid.UUID, i int) []uuid.UUID {
	t[i] = t[len(t)-1]
	return t[:len(t)-1]
}

func removeFromProcessingList(taskID string) {
	core.Base.GetRedis("cache").SRem(context.Background(), "scheduler:processing", taskID)
}

func loadTask(ctx context.Context, taskID string) *Task {
	dbTask, err := core.Base.GetPg("pg").
		Task.
		Query().
		WithProfileGroup().
		WithProduct().
		WithProxyList().
		WithTaskGroup().
		Where(
			task2.ID(
				sonic.UUIDParser(taskID),
			),
		).
		First(ctx)

	if err != nil {
		removeFromProcessingList(taskID)
		return nil
	}

	user, err := dbTask.
		QueryProfileGroup().
		QueryApp().
		QueryUser().
		First(ctx)

	ctx, cancel := context.WithCancel(ctx)

	task := &Task{
		Task:              dbTask,
		subscriptionToken: dbTask.ID.String(),
		controlToken:      helpers.SHA1(dbTask.ID.String()),
		taskID:            dbTask.ID.String(),
		userID:            user.ID.String(),
		startMutex:        &sync.Mutex{},
		dataLock:          &sync.Mutex{},
		site:              dbTask.Edges.Product[0].Site,
		ctx:               ctx,
		cancel:            cancel,
		startTime:         *dbTask.StartTime,
	}

	return task
}
