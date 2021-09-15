package scheduler

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/google/uuid"
	"github.com/prometheus/common/log"
	"sync"
	"time"
)

type Schedule struct {
	data        map[time.Time][]*Task
	taskLockers map[uuid.UUID]*sync.Mutex
	locker      *sync.Mutex
	ctx         context.Context
	cancelFunc  context.CancelFunc
}

//NewScheduler creates a new Schedule object with a cancel func
func NewScheduler() *Schedule {
	ctx, cancel := context.WithCancel(context.Background())
	return &Schedule{ctx: ctx, cancelFunc: cancel}
}

//init initializes the scheduler by creating a new data map, populating the map and processing the tasks
func (s *Schedule) init() {
	defer func() {
		if a := recover(); a != nil {
			fmt.Println("Recovered, terminating all tasks")
			for _, tasks := range s.data {
				for _, task := range tasks {
					fmt.Println("Deallocating Task: ", task.taskID)
					go removeFromProcessingList(task.taskID)
				}
			}
		}
	}()

	s.data = map[time.Time][]*Task{}
	s.locker = &sync.Mutex{}
	s.taskLockers = map[uuid.UUID]*sync.Mutex{}

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
			for startTime := range s.data {
				if time.Since(startTime) >= -time.Second*2 {
					for i := range s.data[startTime] {
						if s.data[startTime][i].taskStarted {
							continue
						}

						if err := s.data[startTime][i].start(s.ctx); err != nil {
							log.Error("error starting task", err, "task_id: ", s.data[startTime][i].ID.String())
						}
					}
				}
			}
		}

	}()
}

//add appends the task to the appropriate task slice in data
func (s *Schedule) add(taskID string) {
	select {
	case <-s.ctx.Done():
		return
	default:
		break
	}

	task := s.loadTask(taskID)

	s.locker.Lock()
	defer s.locker.Unlock()

	//loop through the data to check if task already exists
	for t := range s.data {
		for i := range s.data[t] {
			if s.data[t][i].ID == task.ID && s.data[t][i].StartTime == task.StartTime {
				if s.data[t][i].taskStarted {
					go s.data[t][i].stop()
				}
				s.data[t][i] = task
				return
			} else if s.data[t][i] != nil {
				s.data[t] = removeTask(s.data[t], i)
				goto addTask
			}
		}
		if len(s.data[t]) == 0 {
			delete(s.data, t)
		}
	}
	//append task to the correct data slice
addTask:
	s.data[*task.StartTime] = append(s.data[*task.StartTime], task)
	go task.getPayload()
}

//deleteOlderEntries checks the data set every 15 minutes for any map keys that have exceeded the 1 hour task timeout
func (s *Schedule) deleteOlderEntries() {
	for range time.Tick(time.Second) {
		select {
		case <-s.ctx.Done():
			return
		default:
			break
		}
		s.locker.Lock()

		for startTime, tasks := range s.data {
			if startTime.Sub(time.Now()) >= time.Minute {
				delete(s.data, startTime)
			}

			for i := range tasks {
				s.taskLockers[tasks[i].ID].Lock()
				if tasks[i].stopped {
					s.data[startTime] = removeTask(tasks, i)
				}
				s.taskLockers[tasks[i].ID].Unlock()
				delete(s.taskLockers, tasks[i].ID)
			}

		}

		//for _, tasks := range s.

		s.locker.Unlock()
	}
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
			var wg sync.WaitGroup

			tasks := s.getTasks(k)
			rdb := core.Base.GetRedis("cache")

			pipe := rdb.Pipeline()

			for _, tk := range tasks {
				wg.Add(1)
				tk := tk
				go func() {
					defer wg.Done()
					mID := tk.getMonitorID()
					tk.monitorChannel = mID
					uniqueTasks.Store(mID, tk)

					proxylist, err := tk.Edges.ProxyListOrErr()
					if err != nil {
						log.Error("load proxy list for task: ", tk.taskID, " error: ", err)
						return
					}

					proxies, _ := proxylist[0].Proxies(tk.ctx)

					var redisKey = string("proxies:" + tk.site)

					for _, proxy := range proxies {
						if proxy.Username != "" && proxy.Password != "" {
							pipe.Publish(tk.ctx, redisKey, fmt.Sprintf("%s:%s@%s:%s", proxy.Username, proxy.Password, proxy.IP, proxy.Port))
							continue
						}
						pipe.Publish(tk.ctx, redisKey, fmt.Sprintf("%s:%s", proxy.IP, proxy.Port))
					}

				}()
			}
			wg.Wait()

			_, err := pipe.Exec(context.Background())
			if err != nil {
				log.Error("error sending proxies: ", err)
			}

			uniqueTasks.Range(func(key, value interface{}) bool {
				tk := value.(*Task)
				if !tk.monitorStarted {
					if err := tk.startMonitor(s.ctx); err != nil {
						log.Error("error starting monitor:", err)
						return true
					}
					tk.monitorStarted = true
				} else {
					return true
				}

				go func() {
					for _, tsk := range tasks {
						if tsk.monitorChannel == tk.monitorChannel {
							tsk.monitorStarted = true
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
	rdb := core.Base.GetRedis("cache")

	go func() {
		pubSub := rdb.Subscribe(s.ctx, "scheduler:tasks-updated")

		for taskID := range pubSub.Channel() {
			for _, tasks := range s.data {
				for _, task := range tasks {
					if task.ID.String() == taskID.Payload {
						go s.add(taskID.Payload)
					}
				}
			}
		}

	}()

	for range time.Tick(time.Millisecond * 25) {
		newTask := rdb.SPop(s.ctx, "scheduler:scheduled").Val()
		if newTask == "" {
			continue
		}
		rdb.SAdd(s.ctx, "scheduler:processing", newTask)
		log.Info("Loading Task: ", newTask)
		go s.add(newTask)

	}

}

//getUserTasks retrieves the current user tasks that are appended to the data pool
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

//getData is a debug method that returns all the data of the scheduler
func (s *Schedule) getData() map[time.Time][]*Task {
	s.locker.Lock()
	defer s.locker.Unlock()

	var tasks = map[time.Time][]*Task{}

	for k, tks := range s.data {
		for _, tk := range tks {
			tasks[k] = append(tasks[k], tk)
		}
	}

	return tasks
}

func (s *Schedule) getTasks(t time.Time) []*Task {
	var tasks []*Task
	if tks, ok := s.data[t]; ok {
		for _, tk := range tks {
			tasks = append(tasks, tk)
		}
	}
	return tasks
}
