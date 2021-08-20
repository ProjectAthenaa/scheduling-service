package scheduler

import (
	"context"
	"errors"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	module "github.com/ProjectAthenaa/sonic-core/protos"
	"github.com/ProjectAthenaa/sonic-core/sonic"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"
	tasks "github.com/ProjectAthenaa/sonic-core/task_controller"
	"github.com/json-iterator/go"
	"github.com/prometheus/common/log"
	"sort"
	"strings"
)

var (
	client = helpers.GetTaskControllerClient()
	rdb    = core.Base.GetRedis("cache")
	json   = jsoniter.ConfigFastest
)

//Task is a superset of ent.Task, it holds scheduler-specific fields
type Task struct {
	*ent.Task
	monitorChannel    string `json:"monitor_channel"`
	subscriptionToken string `json:"subscription_token"`
	controlToken      string `json:"control_token"`
	userID            string `json:"user_id"`
	taskID            string `json:"task_id"`
	ctx               context.Context
	cancel            context.CancelFunc
	backend           tasks.Tasks_TaskClient
	monitorStarted    bool `json:"monitor_started"`
	taskStarted       bool
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
	t.backend, err = client.Task(t.ctx)
	if err != nil {
		t.setError(err)
		t.stop()
		return
	}

	if err = t.backend.Send(t.getPayload()); err != nil {
		t.setError(err)
		t.stop()
		return
	}

	t.taskStarted = true

	go t.updateListener()
	t.commandListener()
}

//getPayload retrieves the initial payload needed to start the task
func (t *Task) getPayload() *tasks.Client {
	return &tasks.Client{
		Command:        module.COMMAND_START,
		TaskID:         &t.taskID,
		MonitorChannel: &t.monitorChannel,
	}
}

//updateListener listens to updates from the task controller and forwards them to a redis pubsub channel
func (t *Task) updateListener() {
	var update *tasks.Status
	var err error
	var payload []byte
	go func() {
		for {
			update, err = t.backend.Recv()
			if err != nil {
				log.Error("receive update error: ", err)
				t.stop()
				return
			}

			payload, err = json.Marshal(&update)
			if err != nil {
				log.Error("error marshaling update", err)
				t.setError(err)
			}

			rdb.Publish(t.ctx, fmt.Sprintf("tasks:updates:%s", t.subscriptionToken), string(payload))
		}
	}()
}

//commandListener listens to commands from the redis pubsub and forwards them to the task controller
func (t *Task) commandListener() {
	go func() {
		pubSub := rdb.Subscribe(t.ctx, fmt.Sprintf("tasks:command:%s", t.controlToken))
		commands := pubSub.Channel()
		defer pubSub.Close()

		for cmd := range commands {
			payload := cmd.Payload

			if err := t.backend.Send(&tasks.Client{
				Command: module.COMMAND(module.COMMAND_value[payload]),
			}); err != nil {
				log.Error("error sending update:", err)
				t.setError(err)
			}

			if payload == "STOP" {
				t.stop()
			}
		}
	}()
}

//setError is a wrapper around sending an error to pubsub
func (t *Task) setError(err error) {
	payload, err := json.Marshal(&tasks.Status{
		Status: module.STATUS_ERROR,
		Error:  sonic.ErrString(errors.New(err.Error())),
	})
	rdb.Publish(t.ctx, fmt.Sprintf("tasks:updates:%s", t.subscriptionToken), string(payload))
}

//stop cancels the context therefore stopping the task
func (t *Task) stop() {
	t.cancel()
	t.taskStarted = false
}
