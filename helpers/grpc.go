package helpers

import (
	controller "github.com/ProjectAthenaa/sonic-core/task_controller"
	"google.golang.org/grpc"
	"runtime"
)

func GetTaskControllerClient() controller.TasksClient {
	var conn grpc.ClientConnInterface
	if runtime.GOOS == "linux" {
		conn, _ = grpc.Dial("task-controller.general.svc.cluster.local:3000", grpc.WithInsecure())
	} else {
		conn, _ = grpc.Dial("localhost:3000", grpc.WithInsecure())
	}

	return controller.NewTasksClient(conn)
}
