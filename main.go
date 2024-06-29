package main

import (
	"context"
	"log/slog"

	config "github.com/DggHQ/dggarchiver-config/controller"
	"github.com/DggHQ/dggarchiver-controller/orchestration"
	"github.com/DggHQ/dggarchiver-controller/orchestration/docker"
	"github.com/DggHQ/dggarchiver-controller/orchestration/k8s"
)

func main() {
	ctx := context.Background()

	cfg := config.New()

	var o orchestration.Backend

	if cfg.Controller.K8s.Enabled {
		slog.Info("running", slog.String("mode", "k8s"))
		o = k8s.New(cfg)
	} else {
		slog.Info("running", slog.String("mode", "docker"))
		o = docker.New(cfg)
	}

	go o.Listen(ctx, cfg)

	var forever chan struct{}
	<-forever
}
