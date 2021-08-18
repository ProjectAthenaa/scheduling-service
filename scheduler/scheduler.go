package scheduler

import "github.com/ProjectAthenaa/scheduling-service/graph/model"

var scheduler = NewScheduler()

func init() {
	scheduler.init()
}

//Retrieves scheduled tasks
func GetUserTasks(userID string) []*model.Task  {
	return scheduler.getUserTasks(userID)
}
