package scheduler

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/sonic-core/protos/module"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/go-co-op/gocron"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/common/log"
	"strings"
	"sync"
	"time"
)

var rdb redis.UniversalClient

type Scheduler struct {
	ctx        context.Context
	cancel     context.CancelFunc
	cancellers *sync.Map
	tasks      *sync.Map
	*gocron.Scheduler
}

func init() {
	rdb = core.Base.GetRedis("cache")
}

func NewScheduler() *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	s := &Scheduler{
		ctx:        ctx,
		cancel:     cancel,
		cancellers: &sync.Map{},
		tasks:      &sync.Map{},
		Scheduler:  gocron.NewScheduler(time.UTC),
	}

	if _, err := s.SingletonMode().Every(1).Millisecond().Do(s.loadTasks); err != nil {
		log.Fatalln("error starting task loader: ", err)
	}

	go s.commandListener()
	go s.statusListener()
	go s.taskRequestListener()

	s.StartAsync()

	return &Scheduler{
		ctx:        ctx,
		cancel:     cancel,
		cancellers: &sync.Map{},
		Scheduler:  gocron.NewScheduler(time.UTC),
	}
}

func (s *Scheduler) loadTasks() {
	taskID := rdb.SPop(s.ctx, "scheduler:new-tasks").Val()
	if taskID == "" {
		return
	}

	if _, exists := s.tasks.Load(taskID); exists {
		return
	}

	task := LoadTask(s.ctx, taskID)

	monitorJob, err := s.StartAt(task.monitorStartTime).Do(task.startMonitor, s.ctx)
	if err != nil {
		log.Error("error scheduling task monitor: ", err)
		return
	}

	monitorJob.Tag(task.ID.String())

	job, err := s.StartAt(*task.StartTime).Do(task.Start, task.ctx)
	if err != nil {
		log.Error("error scheduling task: ", err)
		return
	}

	log.Info("Task Loaded | ", task.ID.String(), " | Start In ", task.StartTime.Sub(time.Now()))

	job.Tag(task.controlToken, task.ID.String(), task.userID)
	s.cancellers.Store(task.controlToken, task.cancel)
}

func (s *Scheduler) commandListener() {
	for command := range rdb.PSubscribe(s.ctx, "tasks:commands:*").Channel() {
		cmd := command.Payload
		if cmd != "STOP" {
			continue
		}

		controlToken := strings.Split(command.Channel, ":")[2]
		go func() {
			if cancel, ok := s.cancellers.LoadAndDelete(controlToken); ok {
				for _, job := range s.Jobs() {
					if job.Tags()[0] == controlToken {
						taskID := job.Tags()[1]
						rdb.SRem(s.ctx, "scheduler:processing", taskID)
					}
				}

				cancel.(context.CancelFunc)()

			}
		}()
	}
}

func (s *Scheduler) statusListener() {
	var status *module.Status

	for update := range rdb.PSubscribe(s.ctx, "tasks:updates:*").Channel() {
		if err := json.Unmarshal([]byte(update.Payload), &status); err != nil {
			log.Error("error parsing update for task: ", strings.Split(update.Channel, ":")[2])
		}

		switch status.Status {
		case module.STATUS_STOPPED, module.STATUS_CHECKED_OUT, module.STATUS_ERROR, module.STATUS_CHECKOUT_DECLINE:
			break
		default:
			continue
		}

		taskID := strings.Split(update.Channel, ":")[2]
		s.tasks.Delete(taskID)

		go func() {
			for _, job := range s.Jobs() {
				if job.Tags()[1] == taskID {
					if cancel, ok := s.cancellers.LoadAndDelete(job.Tags()[0]); ok {
						cancel.(context.CancelFunc)()
					}
				}
			}
		}()
	}
}

func (s *Scheduler) getUserTasks(userID string) []*model.Task {
	key := fmt.Sprintf("tasks:%s", userID)
	var tasks []*model.Task

	rdb.Publish(s.ctx, "tasks:user-tasks", userID)
	time.Sleep(time.Second * 2)

	tasksPayloads := rdb.SMembers(s.ctx, key).Val()

	go rdb.Del(s.ctx, key)

	for _, taskPayload := range tasksPayloads {
		var task *model.Task
		if err := json.Unmarshal([]byte(taskPayload), &task); err != nil {
			log.Error("error unmarshalling payload: ", err)
			continue
		}
		tasks = append(tasks, task)
	}

	return tasks
}

func (s *Scheduler) taskRequestListener() {
	for request := range rdb.Subscribe(s.ctx, "tasks:user-tasks").Channel() {
		userID := request.Payload
		go func() {
			ctx, _ := context.WithTimeout(s.ctx, time.Second)
			pipe := rdb.Pipeline()
			for _, job := range s.Jobs() {
				select {
				case <-ctx.Done():
					if _, err := pipe.Exec(s.ctx); err != nil {
						log.Error("error executing pipeline: ", err)
					}
					return
				default:
					if len(job.Tags()) < 2 {
						continue
					}
					if userID == job.Tags()[2] {
						task := &model.Task{
							ID:                job.Tags()[1],
							SubscriptionToken: job.Tags()[1],
							ControlToken:      job.Tags()[0],
							StartTime:         job.NextRun(),
						}

						payload, err := json.Marshal(&task)
						if err != nil {
							continue
						}
						pipe.SAdd(s.ctx, fmt.Sprintf("tasks:%s", userID), string(payload))
					}
				}
			}
			if _, err := pipe.Exec(s.ctx); err != nil {
				log.Error("error executing pipeline: ", err)
			}
		}()
	}
}
