package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	config "github.com/DggHQ/dggarchiver-config/controller"
	"github.com/DggHQ/dggarchiver-controller/notifications"
	"github.com/DggHQ/dggarchiver-controller/orchestration"
	dggarchivermodel "github.com/DggHQ/dggarchiver-model"
	"github.com/containrrr/shoutrrr/pkg/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/nats-io/nats.go"
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
	proxy     string
}

func New(cfg *config.Config) *Docker {
	return &Docker{
		dockerCfg: cfg.Controller.Docker,
		image:     cfg.Controller.WorkerImage,
		natsHost:  cfg.NATS.Host,
		natsTopic: cfg.NATS.Topic,
		proxy:     cfg.ProxyURL,
	}
}

func (d *Docker) ListWorkers(ctx context.Context) ([]orchestration.Worker, error) {
	containers, err := d.dockerCfg.DockerSocket.ContainerList(ctx, container.ListOptions{
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
	containerName := fmt.Sprintf("dggarchiver-worker-%s", vod.VID)

	var livestreamURL string
	switch vod.Platform {
	case "youtube":
		livestreamURL = fmt.Sprintf("https://youtu.be/%s", vod.VID)
	case "rumble", "kick":
		livestreamURL = vod.PlaybackURL
	}

	ctr, err := d.dockerCfg.DockerSocket.ContainerCreate(ctx, &container.Config{
		Image: d.image,
		Env: []string{
			fmt.Sprintf("LIVESTREAM_INFO=%s", data),
			fmt.Sprintf("LIVESTREAM_ID=%s", vod.VID),
			fmt.Sprintf("LIVESTREAM_URL=%s", livestreamURL),
			fmt.Sprintf("LIVESTREAM_PLATFORM=%s", vod.Platform),
			fmt.Sprintf("LIVESTREAM_DOWNLOADER=%s", vod.Downloader),
			fmt.Sprintf("QUALITY=%s", vod.Quality),
			fmt.Sprintf("NATS_HOST=%s", d.natsHost),
			fmt.Sprintf("NATS_TOPIC=%s", d.natsTopic),
			fmt.Sprintf("DOWNLOAD_PROXY=%s", d.proxy),
			"VERBOSE=true",
		},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.Type(d.dockerCfg.Mount.Type),
				Source: d.dockerCfg.Mount.Source,
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

	if err := d.dockerCfg.DockerSocket.ContainerStart(ctx, ctr.ID, container.StartOptions{}); err != nil {
		return errors.Join(ErrUnableToStart, err)
	}

	return nil
}

func (d *Docker) Listen(ctx context.Context, cfg *config.Config) {
	// Subscribe to NATS asynchronously and listen for new jobs and start them once a new job is detected
	if _, err := cfg.NATS.NatsConnection.Subscribe(fmt.Sprintf("%s.job", cfg.NATS.Topic), func(msg *nats.Msg) {
		vod := &dggarchivermodel.VOD{}
		if err := json.Unmarshal(msg.Data, vod); err != nil {
			slog.Error("unable to unmarshal VOD",
				slog.String("vod", string(msg.Data)),
				slog.Any("err", err),
			)
			return
		}

		slog.Info("VOD received", slog.Group("vod", slog.String("id", vod.VID), slog.String("platform", vod.Platform), slog.String("downloader", vod.Downloader)))

		if cfg.Notifications.Condition("receive") {
			errs := cfg.Notifications.Sender.Send(notifications.GetReceiveMessage(vod), &types.Params{
				"title": "Preparing to start container",
			})
			for _, err := range errs {
				if err != nil {
					slog.Warn("unable to send notification", slog.Any("vod", vod), slog.Any("err", err))
				}
			}
		}

		if err := d.StartWorker(ctx, msg.Data, vod); err != nil {
			slog.Error("unable to start worker", slog.Any("err", err))
			return
		}

		if cfg.Notifications.Condition("container") {
			errs := cfg.Notifications.Sender.Send(notifications.GetContainerMessage(vod, fmt.Sprintf("dggarchiver-worker-%s", vod.VID)), &types.Params{
				"title": "Started container",
			})
			for _, err := range errs {
				if err != nil {
					slog.Warn("unable to send notification", slog.Any("vod", vod), slog.Any("err", err))
				}
			}
		}
	}); err != nil {
		slog.Error("unable to subscribe to NATS topic", slog.Any("err", err))
		os.Exit(1)
	}
}
