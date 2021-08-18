package scheduler

import (
	"context"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"sync"
	"time"
)

type Schedule struct {
	data   map[time.Time]Tasks
	locker sync.Mutex
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
		for k, _ := range s.data {
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
	for k, _ := range s.data {
		if k.Sub(time.Now()) <= time.Second*5 {
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
						uniqueTasks.Store(mID, task.Edges.Product[0])
					}
				}()
			}

			wg.Wait()

			uniqueTasks.Range(func(key, value interface{}) bool {

				return true
			})

		}
	}

}

func (s *Schedule) populate() {
	go s.deleteOlderEntries()
	for range time.Tick(time.Millisecond * 200) {
		tasks, err := core.Base.GetPg("pg").Task.Query().WithProduct().All(context.Background())
		if err != nil {
			continue
		}

		for _, task := range tasks {
			if task.StartTime == nil {
				continue
			}

			schedule.add(&Task{
				Task:              task,
				subscriptionToken: task.ID.String(),
				controlToken:      helpers.SHA1(task.ID.String()),
			})
		}
	}
}
