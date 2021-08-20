package graph

type Operation string

const (
	GetScheduledTasks Operation = "getScheduledTasks"
	SendCommand       Operation = "sendCommand"
	TaskUpdates       Operation = "taskUpdates"
)
