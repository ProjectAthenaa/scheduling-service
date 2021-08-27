package scheduler

import (
	"context"
	monitorProtos "github.com/ProjectAthenaa/sonic-core/protos/monitorController"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"
	"google.golang.org/grpc"
)

var monitorClient = getMonitorClient()

//getMonitorClient returns the cient for the monitor controller
func getMonitorClient() monitorProtos.MonitorClient {
	conn, err := grpc.Dial("controller.general.svc.cluster.local:3000", grpc.WithInsecure())
	if err != nil {
		panic(err)
	}

	return monitorProtos.NewMonitorClient(conn)
}

//startMonitor provides a convenient wrapper around building the monitor controller payload
func (t *Task) startMonitor(ctx context.Context) error {
	//checks to see whether there is at least one subscriber for given key,
	//if there is it means the monitor has already started
	if subCount, err := core.Base.GetRedis("cache").PubSubNumSub(t.ctx, t.monitorChannel).Result(); err != nil {
		if subCount[t.monitorChannel] >= 1 {
			return nil
		}
	}

	newMonitorTask := &monitorProtos.Task{
		Site:         string(t.Edges.Product[0].Site),
		Metadata:     t.Edges.Product[0].Metadata,
		RedisChannel: t.monitorChannel,
	}

	switch t.Edges.Product[0].LookupType {
	case product.LookupTypeKeywords:
		newMonitorTask.Lookup = &monitorProtos.Task_Keywords{Keywords: &monitorProtos.Keywords{
			Positive: t.Edges.Product[0].PositiveKeywords,
			Negative: t.Edges.Product[0].NegativeKeywords,
		}}
	case product.LookupTypeLink:
		newMonitorTask.Lookup = &monitorProtos.Task_Link{Link: t.Edges.Product[0].Link}
	case product.LookupTypeOther:
		break
	}

	_, err := monitorClient.NewTask(ctx, newMonitorTask)
	if err != nil {
		return err
	}

	return nil
}
