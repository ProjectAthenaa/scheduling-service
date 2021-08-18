package scheduler

import (
	"context"
	monitorProtos "github.com/ProjectAthenaa/sonic-core/monitor_controller"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"
	"google.golang.org/grpc"
)

var monitorClient = getMonitorClient()

func getMonitorClient() monitorProtos.MonitorClient {
	conn, err := grpc.Dial("controller.general.svc.cluster.local:3000", grpc.WithInsecure())
	if err != nil {
		panic(err)
	}

	return monitorProtos.NewMonitorClient(conn)
}

func (t *Task) StartMonitor(ctx context.Context) error {
	newMonitorTask := &monitorProtos.Task{
		Site:     string(t.Edges.Product[0].Site),
		Lookup:   nil,
		Metadata: t.Edges.Product[0].Metadata,
	}

	switch t.Edges.Product[0].LookupType {
	case product.LookupTypeKeywords:
		newMonitorTask.Lookup = &monitorProtos.Task_Keywords{Keywords: &monitorProtos.Keywords{
			Positive: t.Edges.Product[0].PositiveKeywords,
			Negative: t.Edges.Product[0].NegativeKeywords,
		}}
	case product.LookupTypeLink:
		newMonitorTask.Lookup = &monitorProtos.Task_Link{Link: t.Edges.Product[0].Link}
	}

	_, err := monitorClient.NewTask(ctx, newMonitorTask)
	if err != nil {
		return err
	}

	//t = resp
	return nil
}
