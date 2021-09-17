package scheduler

import (
	"context"
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
		Scheduler:  gocron.NewScheduler(time.UTC),
	}

	if _, err := s.Every(100).Milliseconds().Do(s.loadTasks); err != nil {
		log.Fatalln("error starting task loader: ", err)
	}

	go s.commandListener()
	go s.statusListener()

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

	task := loadTask(s.ctx, taskID)

	job, err := s.StartAt(*task.StartTime).Do(task.start, task.ctx)
	if err != nil {
		log.Error("error scheduling task: ", err)
		return
	}
	job.Tag(task.controlToken, task.ID.String())
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
