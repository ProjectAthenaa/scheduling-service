package scheduler

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/graph/model"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/prometheus/common/log"
	"sync"
	"time"
)

type Schedule struct {
	tasks      []*Task
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
	defer func() {
		if a := recover(); a != nil {
			fmt.Println("Recovered, terminating all tasks")
			for i := range s.tasks {
				fmt.Println("Deallocating Task: ", s.tasks[i])
				go removeFromProcessingList(s.tasks[i].ID.String())
			}
		}
	}()

	s.locker = &sync.Mutex{}
	s.tasks = []*Task{}

	go func() {
		//start population as a goroutine
		go s.populate()
		//check for new to-start tasks every 200ms
		for {
			select {
			case <-s.ctx.Done():
				return
			default:
				break
			}

			//startMonitors for current data set
			go s.startMonitors()

			//start tasks
			for i := range s.tasks {
				if s.tasks[i].taskStarted {
					continue
				}

				if time.Since(s.tasks[i].startTime) >= -time.Second*2 {
					s.tasks[i].start(s.ctx)
				}

			}
		}

	}()
}

//add appends the task to the appropriate task slice in data
func (s *Schedule) add(taskID string) {
	defer func() {
		log.Info("Loaded Task | ", taskID)
	}()
	select {
	case <-s.ctx.Done():
		return
	default:
		break
	}

	task := s.loadTask(taskID)

	s.locker.Lock()
	defer s.locker.Unlock()

	for i := range s.tasks {
		if s.tasks[i].ID == task.ID {
			s.tasks[i] = task
			return
		}
	}

	go task.getPayload()
}

//deleteOlderEntries checks the data set every 15 minutes for any map keys that have exceeded the 1 hour task timeout
func (s *Schedule) deleteOlderEntries() {
	defer func() {
		if a := recover(); a != nil {
		}
	}()
	select {
	case <-s.ctx.Done():
		return
	default:
		break
	}
	s.locker.Lock()
	defer s.locker.Unlock()

	for i := range s.tasks {
		if s.tasks[i].stopped {
			s.tasks = removeTask(s.tasks, i)
			continue
		}
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

	rdb := core.Base.GetRedis("cache")
	pipe := rdb.Pipeline()
	var wg  = &sync.WaitGroup{}
	var uniqueTasks = &sync.Map{}

	for _, tk := range s.tasks {
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
			for i := range s.tasks {
				if s.tasks[i].monitorChannel == tk.monitorChannel {
					s.tasks[i].monitorStarted = true
				}
			}
		}()

		return true
	})

}

//populate spawns the deleteOlderEntries func as a goroutine, creates a goroutine to listen for deleted tasks, and retrieves
//all tasks from the database
func (s *Schedule) populate() {
	rdb := core.Base.GetRedis("cache")

	go func() {
		pubSub := rdb.Subscribe(s.ctx, "scheduler:tasks-updated")

		for taskID := range pubSub.Channel() {
			for i := range s.tasks {
				if s.tasks[i].ID.String() == taskID.Payload {
					go s.add(taskID.Payload)
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
		go s.add(newTask)

	}

}

//getUserTasks retrieves the current user tasks that are appended to the data pool
func (s *Schedule) getUserTasks(userID string) (tasks []*model.Task) {
	s.locker.Lock()
	defer s.locker.Unlock()
	for i := range s.tasks {
		if s.tasks[i].userID == userID {
			tasks = append(tasks, &model.Task{
				ID:                s.tasks[i].taskID,
				SubscriptionToken: s.tasks[i].subscriptionToken,
				ControlToken:      s.tasks[i].controlToken,
				StartTime:         s.tasks[i].startTime,
			})
		}
	}
	return
}

//getData is a debug method that returns all the data of the scheduler
func (s *Schedule) getData() map[time.Time][]*Task {
	s.locker.Lock()
	defer s.locker.Unlock()

	var tasks = map[time.Time][]*Task{}

	for i := range s.tasks {
		tasks[s.tasks[i].startTime] = append(tasks[s.tasks[i].startTime], s.tasks[i])
	}

	return tasks
}

func (s *Schedule) getTasks(t time.Time) []*Task {
	var tasks []*Task
	for i := range s.tasks {
		if s.tasks[i].startTime == t {
			tasks = append(tasks, s.tasks[i])
		}
	}
	return tasks
}
