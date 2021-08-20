package scheduler

import (
	"context"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/task"
	"github.com/prometheus/common/log"
	"sync"
	"time"
)

type Schedule struct {
	data       map[time.Time][]*Task
	locker     sync.Mutex
	ctx        context.Context
	cancelFunc context.CancelFunc
}

func NewScheduler() *Schedule {
	ctx, cancel := context.WithCancel(context.Background())
	return &Schedule{ctx: ctx, cancelFunc: cancel}
}

func (s *Schedule) init() {
	s.data = map[time.Time][]*Task{}
	go func() {
		go s.populate()
		for range time.Tick(time.Millisecond * 200) {
			select {
			case <-s.ctx.Done():
				return
			default:
				break
			}

			go s.startMonitors()
			for _, t := range s.getChunks() {
				if err := t.start(s.ctx); err != nil {
					log.Error("error starting task", err, "task_id:", t.ID.String())
				}
			}
		}
	}()
}

func (s *Schedule) add(task *Task) {
	select {
	case <-s.ctx.Done():
		return
	default:
		break
	}
	s.locker.Lock()
	defer s.locker.Unlock()

	for _, tks := range s.data {
		for _, tk := range tks {
			if tk.ID == task.ID {
				tk = task
				return
			}
		}
	}

	s.data[*task.StartTime] = append(s.data[*task.StartTime], task)
}

func (s *Schedule) deleteOlderEntries() {
	for range time.Tick(time.Minute * 15) {
		select {
		case <-s.ctx.Done():
			return
		default:
			break
		}
		s.locker.Lock()
		for k := range s.data {
			if time.Now().Sub(k) >= time.Hour {
				delete(s.data, k)
			}
		}
		s.locker.Unlock()
	}
}

func (s *Schedule) getChunks() []*Task {
	select {
	case <-s.ctx.Done():
		return nil
	default:
		break
	}
	var tasks []*Task
	s.locker.Lock()
	defer s.locker.Unlock()
	for k := range s.data {
		if k.Sub(time.Now()) <= time.Second*1 {
			chunks := chunk(s.data[k], helpers.GetProcessCount())
			tasks = append(tasks, chunks[helpers.GetCurrentProcessNumber()]...)
		}
	}
	return tasks
}

func (s *Schedule) startMonitors() {
	select {
	case <-s.ctx.Done():
		return
	default:
		break
	}
	s.locker.Lock()
	defer s.locker.Unlock()

	for k, tasks := range s.data {
		if k.Sub(time.Now()) <= time.Minute {
			var uniqueTasks sync.Map
			chunks := chunk(tasks, 4)

			var wg sync.WaitGroup

			for i := 0; i < len(chunks); i++ {
				wg.Add(1)
				index := i
				go func() {
					defer wg.Done()
					for _, tk := range chunks[index] {
						mID := tk.getMonitorID()
						tk.monitorChannel = mID
						uniqueTasks.Store(mID, tk)
					}
				}()
			}

			wg.Wait()

			uniqueTasks.Range(func(key, value interface{}) bool {
				tk := value.(*Task)
				if !tk.monitorStarted {
					if err := tk.startMonitor(s.ctx); err != nil {
						//log.Error("error starting monitor:", err)
						return true
					}
					tk.monitorStarted = true
				}

				return true
			})

		}
	}
}

func (s *Schedule) populate() {
	go s.deleteOlderEntries()

	go func() {
		pubSub := core.Base.GetRedis("cache").Subscribe(s.ctx, "scheduler:tasks-deleted")
		defer pubSub.Close()
		for taskID := range pubSub.Channel() {
			s.locker.Lock()
			for k, tasks := range s.data {
				for i, tk := range tasks {
					if tk.taskID == taskID.Payload {
						s.data[k] = removeTask(tasks, i)
					}
				}
			}
			s.locker.Unlock()
		}
	}()

	for range time.Tick(time.Millisecond * 200) {
		select {
		case <-s.ctx.Done():
			return
		default:
			break
		}
		tasks, err := core.
			Base.
			GetPg("pg").
			Task.Query().
			Where(
				task.StartTimeGTE(
					time.Now().
						Add(-time.Hour * 50000),
				),
			).
			WithProduct().
			All(s.ctx)

		if err != nil {
			continue
		}

		var wg sync.WaitGroup
		for _, tk := range tasks {
			if tk.StartTime == nil {
				continue
			}

			wg.Add(1)

			t := tk
			go func() {
				defer wg.Done()
				user, err := t.
					QueryProfileGroup().
					QueryApp().
					QueryUser().
					First(s.ctx)
				if err != nil {
					if user == nil {
						core.Base.GetPg("pg").Task.DeleteOne(t).ExecX(context.Background())
					}
					log.Error("error getting user ", err)
					return
				}

				s.add(&Task{
					Task:              t,
					subscriptionToken: t.ID.String(),
					controlToken:      helpers.SHA1(t.ID.String()),
					taskID:            t.ID.String(),
					userID:            user.ID.String(),
				})
			}()
		}
		wg.Wait()
	}
}

func (s *Schedule) getUserTasks(userID string) (tasks []*model.Task) {
	s.locker.Lock()
	defer s.locker.Unlock()
	for k, v := range s.data {
		for _, t := range v {
			if t.userID == userID {
				tasks = append(tasks, &model.Task{
					ID:                t.taskID,
					SubscriptionToken: t.subscriptionToken,
					ControlToken:      t.controlToken,
					StartTime:         k,
				})
			}
		}
	}
	return
}

func (s *Schedule) getData() map[time.Time][]*Task {
	s.locker.Lock()
	defer s.locker.Unlock()
	return s.data
}
