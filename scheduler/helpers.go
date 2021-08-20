package scheduler

//removeTask removes a given task from the index provided
func removeTask(t []*Task, i int) []*Task {
	t[i] = t[len(t)-1]
	return t[:len(t)-1]
}
