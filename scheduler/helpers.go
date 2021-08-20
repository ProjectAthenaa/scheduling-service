package scheduler

func removeTask(t []*Task, i int) []*Task {
	t[i] = t[len(t)-1]
	return t[:len(t)-1]
}
