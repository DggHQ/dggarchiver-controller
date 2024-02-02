package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	config "github.com/DggHQ/dggarchiver-config/controller"
	"github.com/DggHQ/dggarchiver-controller/orchestration"
	"github.com/DggHQ/dggarchiver-controller/util"
	log "github.com/DggHQ/dggarchiver-logger"
	dggarchivermodel "github.com/DggHQ/dggarchiver-model"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/nats-io/nats.go"
	luaLibs "github.com/vadv/gopher-lua-libs"
	lua "github.com/yuin/gopher-lua"
)

var (
	ErrUnableToCreate = errors.New("unable to create worker container")
	ErrUnableToStart  = errors.New("unable to start worker container")
)

type Docker struct {
	dockerCfg config.DockerConfig
	image     string
	natsHost  string
	natsTopic string
}

func New(cfg *config.Config) *Docker {
	return &Docker{
		dockerCfg: cfg.Controller.Docker,
		image:     cfg.Controller.WorkerImage,
		natsHost:  cfg.NATS.Host,
		natsTopic: cfg.NATS.Topic,
	}
}

func (d *Docker) ListWorkers(ctx context.Context) ([]orchestration.Worker, error) {
	containers, err := d.dockerCfg.DockerSocket.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "name",
			Value: "dggarchiver-worker",
		}),
	})
	if err != nil {
		return nil, err
	}

	w := []orchestration.Worker{}
	for _, v := range containers {
		w = append(w, orchestration.Worker{
			ID:     v.ID,
			Name:   v.Names[0],
			Image:  v.Image,
			Status: v.Status,
		})
	}

	return w, nil
}

func (d *Docker) StartWorker(ctx context.Context, data []byte, vod *dggarchivermodel.VOD) error {
	containerName := fmt.Sprintf("dggarchiver-worker-%s", vod.ID)

	var livestreamURL string
	switch vod.Platform {
	case "youtube":
		livestreamURL = fmt.Sprintf("https://youtu.be/%s", vod.ID)
	case "rumble", "kick":
		livestreamURL = vod.PlaybackURL
	}

	container, err := d.dockerCfg.DockerSocket.ContainerCreate(ctx, &container.Config{
		Image: d.image,
		Env: []string{
			fmt.Sprintf("LIVESTREAM_INFO=%s", data),
			fmt.Sprintf("LIVESTREAM_ID=%s", vod.ID),
			fmt.Sprintf("LIVESTREAM_URL=%s", livestreamURL),
			fmt.Sprintf("LIVESTREAM_PLATFORM=%s", vod.Platform),
			fmt.Sprintf("LIVESTREAM_DOWNLOADER=%s", vod.Downloader),
			fmt.Sprintf("NATS_HOST=%s", d.natsHost),
			fmt.Sprintf("NATS_TOPIC=%s", d.natsTopic),
			"VERBOSE=true",
		},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: "dggarchiver-lbrynet_videos",
				Target: "/videos",
			},
		},
		AutoRemove: d.dockerCfg.AutoRemove,
	}, &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			d.dockerCfg.Network: {
				NetworkID: d.dockerCfg.Network,
			},
		},
	}, nil, containerName)
	if err != nil {
		return errors.Join(ErrUnableToCreate, err)
	}

	if err := d.dockerCfg.DockerSocket.ContainerStart(ctx, container.ID, types.ContainerStartOptions{}); err != nil {
		return errors.Join(ErrUnableToStart, err)
	}

	return nil
}

func (d *Docker) Listen(ctx context.Context, cfg *config.Config) {
	L := lua.NewState()
	if cfg.Controller.Plugins.Enabled {
		luaLibs.Preload(L)
		if err := L.DoFile(cfg.Controller.Plugins.PathToPlugin); err != nil {
			log.Fatalf("Wasn't able to load the Lua script: %s", err)
		}
	}

	// Subscribe to NATS asynchronously and listen for new jobs and start them once a new job is detected
	if _, err := cfg.NATS.NatsConnection.Subscribe(fmt.Sprintf("%s.job", cfg.NATS.Topic), func(msg *nats.Msg) {
		vod := &dggarchivermodel.VOD{}
		if err := json.Unmarshal(msg.Data, vod); err != nil {
			log.Errorf("Wasn't able to unmarshal VOD, skipping: %s", err)
			return
		}

		log.Infof("Received a VOD: %s", vod)

		if cfg.Controller.Plugins.Enabled {
			util.LuaCallReceiveFunction(L, vod)
		}

		if err := d.StartWorker(ctx, msg.Data, vod); err != nil {
			log.Errorf("error occured while starting worker, skipping: %s", err)
			return
		}

		if cfg.Controller.Plugins.Enabled {
			util.LuaCallContainerFunction(L, vod, true)
		}
	}); err != nil {
		log.Fatalf("An error occured when subscribing to topic: %s", err)
	}
}
