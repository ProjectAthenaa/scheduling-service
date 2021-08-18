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
	data   map[time.Time]Tasks
	locker sync.Mutex
}

func NewScheduler() *Schedule {
	return &Schedule{}
}

func (s *Schedule) init() {
	go func() {
		go s.populate()
		for range time.Tick(time.Millisecond * 200) {
			go s.startMonitors()
			for _, task := range s.getChunks() {
				if err := task.start(); err != nil {
					log.Error("error starting task", err, "task_id:", task.ID.String())
				}
			}
		}
	}()
}

func (s *Schedule) add(task *Task) {
	s.locker.Lock()
	defer s.locker.Unlock()

	if tasks, ok := s.data[*task.StartTime]; ok {
		for _, t := range tasks {
			if t.ID == task.ID {
				return
			}
		}

		tasks = append(tasks, task)
	} else {
		s.data[*task.StartTime] = Tasks{task}
	}
}

func (s *Schedule) deleteOlderEntries() {
	for range time.Tick(time.Minute * 15) {
		s.locker.Lock()
		for k := range s.data {
			if time.Now().Sub(k) >= time.Hour {
				delete(s.data, k)
			}
		}
		s.locker.Unlock()
	}
}

func (s *Schedule) getChunks() Tasks {
	var tasks Tasks
	s.locker.Lock()
	defer s.locker.Unlock()
	for k := range s.data {
		if k.Sub(time.Now()) <= time.Second*1 {
			chunks := s.data[k].Chunk(helpers.GetProcessCount())
			tasks = append(tasks, chunks[helpers.GetCurrentProcessNumber()]...)
		}
	}
	return tasks
}

func (s *Schedule) startMonitors() {
	s.locker.Lock()
	defer s.locker.Unlock()

	for k, tasks := range s.data {
		if k.Sub(time.Now()) <= time.Minute {
			var uniqueTasks sync.Map
			chunks := tasks.Chunk(4)

			var wg sync.WaitGroup

			for i := 0; i < 4; i++ {
				wg.Add(1)
				index := i
				go func() {
					defer wg.Done()
					for _, task := range chunks[index] {
						mID := task.getMonitorID()
						task.monitorChannel = mID
						uniqueTasks.Store(mID, task)
					}
				}()
			}

			wg.Wait()

			uniqueTasks.Range(func(key, value interface{}) bool {
				task := value.(*Task)
				if err := task.startMonitor(context.Background()); err != nil {
					log.Error("error starting monitor:", err)
				}
				return true
			})

		}
	}
}

func (s *Schedule) populate() {
	go s.deleteOlderEntries()
	for range time.Tick(time.Millisecond * 200) {
		tasks, err := core.Base.GetPg("pg").Task.Query().Where(task.StartTimeGTE(time.Now().Add(-time.Minute * 30))).WithProduct().All(context.Background())
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
				user, err := t.QueryProfileGroup().QueryApp().QueryUser().First(context.Background())
				if err != nil {
					log.Error("error getting user", err)
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
