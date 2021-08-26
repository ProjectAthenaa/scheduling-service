package scheduler

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/sonic-core/protos/module"
	tasks "github.com/ProjectAthenaa/sonic-core/protos/taskController"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"
	"github.com/json-iterator/go"
	"github.com/prometheus/common/log"
	"sort"
	"strings"
	"time"
)

var (
	client = helpers.GetTaskControllerClient()
	rdb    = core.Base.GetRedis("cache")
	json   = jsoniter.ConfigFastest
)

//Task is a superset of ent.Task, it holds scheduler-specific fields
type Task struct {
	*ent.Task
	monitorChannel    string
	subscriptionToken string
	controlToken      string
	userID            string
	taskID            string
	ctx               context.Context
	cancel            context.CancelFunc
	monitorStarted    bool
	taskStarted       bool
	startTime         time.Time
}

//chunk takes in a slice of tasks and a chunkSize and returns a new slice of slices that has the length of chunkSize and contains
//an evenly distributed amount of Task(s)
func chunk(tasks []*Task, chunkSize int) [][]*Task {
	if len(tasks) == 0 {
		return nil
	}
	divided := make([][]*Task, (len(tasks)+chunkSize-1)/chunkSize)
	prev := 0
	i := 0
	till := len(tasks) - chunkSize
	for prev < till {
		next := prev + chunkSize
		divided[i] = tasks[prev:next]
		prev = next
		i++
	}
	divided[i] = tasks[prev:]
	return divided
}

//getMonitorID returns the monitor id of a task based on its lookup values
func (t *Task) getMonitorID() string {
	prefix := fmt.Sprintf("monitors:%s:", t.Edges.Product[0].Site)
	v := t.Edges.Product[0]
	switch v.LookupType {
	case product.LookupTypeLink:
		return prefix + helpers.SHA1(v.Link)
	case product.LookupTypeKeywords:
		sort.Strings(v.PositiveKeywords)
		sort.Strings(v.NegativeKeywords)

		for i, s := range v.PositiveKeywords {
			v.PositiveKeywords[i] = strings.ToLower(s)
		}
		for i, s := range v.NegativeKeywords {
			v.NegativeKeywords[i] = strings.ToLower(s)
		}

		return prefix + helpers.SHA1(strings.Join(v.PositiveKeywords, "")+strings.Join(v.NegativeKeywords, ""))
	case product.LookupTypeOther:
		for k, val := range v.Metadata {
			if strings.Contains(k, "LOOKUP_") {
				return prefix + helpers.SHA1(val)
			}
		}
	}
	return ""
}

//start, calls the internal process method as a goroutine
func (t *Task) start(ctx context.Context) error {
	go t.process(ctx)
	return nil
}

//process is a wrapper around the task controller
func (t *Task) process(ctx context.Context) {
	var err error
	t.ctx, t.cancel = context.WithCancel(ctx)
	resp, err := client.Task(t.ctx, t.getPayload())
	if err != nil {
		log.Error("start task: ", err, t.ID)
		t.taskStarted = false
		return
	}

	if !resp.Started {
		log.Error("Task ", t.ID, " didnt start")
	}

	t.taskStarted = resp.Started
}

//getPayload retrieves the initial payload needed to start the task
func (t *Task) getPayload() *tasks.StartRequest {
	return &tasks.StartRequest{
		TaskID: t.taskID,
		Channels: &module.Channels{
			MonitorChannel:  t.monitorChannel,
			UpdatesChannel:  t.subscriptionToken,
			CommandsChannel: t.controlToken,
		},
	}
}

func (t *Task) stop() {
	core.Base.GetRedis("cache").Publish(t.ctx, fmt.Sprintf("tasks:commands:%s", t.controlToken), "STOP")
}
