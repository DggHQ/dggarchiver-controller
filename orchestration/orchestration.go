package orchestration

import (
	"context"

	config "github.com/DggHQ/dggarchiver-config/controller"
	dggarchivermodel "github.com/DggHQ/dggarchiver-model"
)

type Worker struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

type Backend interface {
	ListWorkers(context.Context) ([]Worker, error)
	StartWorker(context.Context, []byte, *dggarchivermodel.VOD) error
	Listen(context.Context, *config.Config)
}
