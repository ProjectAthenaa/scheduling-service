package scheduler

import "github.com/google/uuid"

//removeTask removes a given task from the index provided
func removeTask(t []*Task, i int) []*Task {
	t[i] = t[len(t)-1]
	return t[:len(t)-1]
}

//removeID removes a given task from the index provided
func removeID(t []uuid.UUID, i int) []uuid.UUID {
	t[i] = t[len(t)-1]
	return t[:len(t)-1]
}
