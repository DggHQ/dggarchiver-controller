package main

import (
	"context"
	"time"

	config "github.com/DggHQ/dggarchiver-config/controller"
	"github.com/DggHQ/dggarchiver-controller/orchestration"
	"github.com/DggHQ/dggarchiver-controller/orchestration/docker"
	"github.com/DggHQ/dggarchiver-controller/orchestration/k8s"
	log "github.com/DggHQ/dggarchiver-logger"
)

func init() {
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		log.Fatalf("%s", err)
	}
	time.Local = loc
}

func main() {
	ctx := context.Background()

	cfg := config.Config{}
	cfg.Load()

	if cfg.Controller.Verbose {
		log.SetLevel(log.DebugLevel)
	}

	var o orchestration.Platform

	if cfg.Controller.K8s.Enabled {
		log.Infof("%s", "Running in Kubernetes Mode.")
		o = k8s.New(&cfg)
	} else {
		log.Infof("%s", "Running in Docker Mode.")
		o = docker.New(&cfg)
	}

	go o.Listen(ctx, &cfg)
	log.Infof("Waiting for VODs...")

	var forever chan struct{}
	<-forever
}
