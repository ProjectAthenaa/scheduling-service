package scheduler

import (
	"context"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/task"
	"github.com/google/uuid"
	"github.com/prometheus/common/log"
	"sync"
	"time"
)

type Schedule struct {
	data       map[time.Time][]uuid.UUID
	tasks      map[uuid.UUID]*Task
	locker     *sync.Mutex
	ctx        context.Context
	cancelFunc context.CancelFunc
}

//NewScheduler creates a new Schedule object with a cancel func
func NewScheduler() *Schedule {
	ctx, cancel := context.WithCancel(context.Background())
	return &Schedule{ctx: ctx, cancelFunc: cancel}
}

//init initializes the scheduler by creating a new data map, populating the map and processing the tasks
func (s *Schedule) init() {
	s.data = map[time.Time][]uuid.UUID{}
	s.tasks = map[uuid.UUID]*Task{}
	s.locker = &sync.Mutex{}

	go func() {
		//start population as a goroutine
		go s.populate()
		//check for new to-start tasks every 200ms
		for range time.Tick(time.Millisecond * 200) {
			select {
			case <-s.ctx.Done():
				return
			default:
				break
			}

			//startMonitors for current data set
			go s.startMonitors()

			//start tasks

			for _, t := range s.getChunks() {
				if t.taskStarted {
					continue
				}
				if err := t.start(s.ctx); err != nil {
					log.Error("error starting task", err, "task_id:", t.ID.String())
				}
			}
		}
	}()
}

//add appends the task to the appropriate task slice in data
func (s *Schedule) add(task *Task) {
	select {
	case <-s.ctx.Done():
		return
	default:
		break
	}

	s.locker.Lock()
	defer s.locker.Unlock()

	//loop through the data to check if task already exists
	for t, ids := range s.data {
		for i, id := range ids {
			if tk := s.tasks[id]; tk.ID == task.ID && tk.startTime == task.startTime {
				tk = task
				return
			} else if tk != nil {
				s.data[t] = removeID(s.data[t], i)
				goto addTask
			}
		}
		if len(s.data[t]) == 0 {
			delete(s.data, t)
		}
	}

	log.Info(task.startTime)

	//append task to the correct data slice
addTask:
	s.data[*task.StartTime] = append(s.data[*task.StartTime], task.ID)
	s.tasks[task.ID] = task
	go task.getPayload()
}

//deleteOlderEntries checks the data set every 15 minutes for any map keys that have exceeded the 1 hour task timeout
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

//getChunks returns the tasks assigned to the current process
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
			var timeTasks []*Task
			for _, id := range s.data[k] {
				timeTasks = append(timeTasks, s.tasks[id])
			}

			chunks := chunk(timeTasks, helpers.GetProcessCount())
			tasks = append(tasks, chunks[helpers.GetCurrentProcessNumber()]...)
		}
	}

	return tasks
}

//startMonitors starts the monitors for each task after first isolating the unique tasks
func (s *Schedule) startMonitors() {
	select {
	case <-s.ctx.Done():
		return
	default:
		break
	}
	s.locker.Lock()
	defer s.locker.Unlock()

	for k := range s.data {
		if k.Sub(time.Now()) <= time.Minute {
			var uniqueTasks sync.Map
			chunks := chunk(s.getTasks(k), 4)

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
						log.Error("error starting monitor:", err)
						return true
					}
					tk.monitorStarted = true
				}

				go func() {
					for _, ch := range chunks {
						for _, t := range ch {
							if t.monitorChannel == tk.monitorChannel {
								t.monitorStarted = true
							}
						}
					}
				}()

				return true
			})

		}
	}
}

//populate spawns the deleteOlderEntries func as a goroutine, creates a goroutine to listen for deleted tasks, and retrieves
//all tasks from the database
func (s *Schedule) populate() {
	go s.deleteOlderEntries()

	go func() {
		pubSub := core.Base.GetRedis("cache").Subscribe(s.ctx, "scheduler:tasks-deleted")
		defer pubSub.Close()
		for taskID := range pubSub.Channel() {
			s.locker.Lock()
			for k, ids := range s.data {
				for i, id := range ids {
					if tk := s.tasks[id]; tk.ID.String() == taskID.Payload {
						delete(s.tasks, id)
						s.data[k] = removeID(ids, i)

						if tk.taskStarted {
							tk.stop()
						}
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
					time.Now(),
				),
				task.StartTimeLTE(time.Now().Add(time.Minute*30)),
				//task.StartTimeNotNil(),
			).
			WithProduct().
			WithProfileGroup().
			WithProxyList().
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
				c, cancel := context.WithCancel(context.Background())

				s.add(&Task{
					Task:              t,
					subscriptionToken: t.ID.String(),
					controlToken:      helpers.SHA1(t.ID.String()),
					taskID:            t.ID.String(),
					userID:            user.ID.String(),
					startMutex:        &sync.Mutex{},
					dataLock:          &sync.Mutex{},
					site:              t.Edges.Product[0].Site,
					ctx:               c,
					cancel:            cancel,
					startTime:         *t.StartTime,
				})
			}()
		}
		wg.Wait()
	}
}

//getUserTasks retrieves the current user tasks that are appended to the data pool
func (s *Schedule) getUserTasks(userID string) (tasks []*model.Task) {
	s.locker.Lock()
	defer s.locker.Unlock()
	for k, v := range s.data {
		for _, t := range v {
			if tk := s.tasks[t]; tk.userID == userID {
				tasks = append(tasks, &model.Task{
					ID:                tk.taskID,
					SubscriptionToken: tk.subscriptionToken,
					ControlToken:      tk.controlToken,
					StartTime:         k,
				})
			}
		}
	}
	return
}

//getData is a debug method that returns all the data of the scheduler
func (s *Schedule) getData() map[time.Time][]*Task {
	s.locker.Lock()
	defer s.locker.Unlock()

	var tasks = map[time.Time][]*Task{}

	for k, ids := range s.data {
		for _, id := range ids {
			tasks[k] = append(tasks[k], s.tasks[id])
		}
	}

	return tasks
}

func (s *Schedule) getTasks(t time.Time) []*Task {
	var tasks []*Task
	if ids, ok := s.data[t]; ok {
		for _, id := range ids {
			tasks = append(tasks, s.tasks[id])
		}
	}
	return tasks
}
