package scheduler

import "github.com/ProjectAthenaa/scheduling-service/graph/model"

var scheduler = NewScheduler()

func init() {
	scheduler.init()
}

//GetUserTasks, retrieves user scheduled tasks
func GetUserTasks(userID string) []*model.Task {
	return scheduler.getUserTasks(userID)
}

//Stop, stops the scheduler
func Stop() {
	scheduler.cancelFunc()
}
