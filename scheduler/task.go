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

type Tasks []*Task

type Task struct {
	*ent.Task
	monitorChannel    string
	subscriptionToken string
	controlToken      string
	userID            string
	taskID            string
	ctx               context.Context
	cancel            context.CancelFunc
	backend           tasks.Tasks_TaskClient
}

func (t Tasks) Chunk(chunkSize int) []Tasks {
	if len(t) == 0 {
		return nil
	}
	divided := make([]Tasks, (len(t)+chunkSize-1)/chunkSize)
	prev := 0
	i := 0
	till := len(t) - chunkSize
	for prev < till {
		next := prev + chunkSize
		divided[i] = t[prev:next]
		prev = next
		i++
	}
	divided[i] = t[prev:]
	return divided
}

func (t *Task) getMonitorID() string {
	v := t.Edges.Product[0]
	switch v.LookupType {
	case product.LookupTypeLink:
		return helpers.SHA1(v.Link)
	case product.LookupTypeKeywords:
		sort.Strings(v.PositiveKeywords)
		sort.Strings(v.NegativeKeywords)

		for i, s := range v.PositiveKeywords {
			v.PositiveKeywords[i] = strings.ToLower(s)
		}
		for i, s := range v.NegativeKeywords {
			v.NegativeKeywords[i] = strings.ToLower(s)
		}

		return helpers.SHA1(strings.Join(v.PositiveKeywords, "") + strings.Join(v.NegativeKeywords, ""))
	case product.LookupTypeOther:
		for k, val := range v.Metadata {
			if strings.Contains(k, "LOOKUP_") {
				return helpers.SHA1(val)
			}
		}
	}
	return ""
}

func (t *Task) start(ctx context.Context) error {
	go t.process(ctx)
	return nil
}

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

	go t.updateListener()
	t.commandListener()
}

func (t *Task) getPayload() *tasks.Client {
	return &tasks.Client{
		Command:        module.COMMAND_START,
		TaskID:         &t.taskID,
		MonitorChannel: &t.monitorChannel,
	}
}

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

func (t *Task) setError(err error) {
	payload, err := json.Marshal(&tasks.Status{
		Status: module.STATUS_ERROR,
		Error:  sonic.ErrString(errors.New(err.Error())),
	})
	rdb.Publish(t.ctx, fmt.Sprintf("tasks:updates:%s", t.subscriptionToken), string(payload))
}

func (t *Task) stop() {
	t.cancel()
}
