package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	config "github.com/DggHQ/dggarchiver-config/controller"
	"github.com/DggHQ/dggarchiver-controller/orchestration"
	"github.com/DggHQ/dggarchiver-controller/util"
	dggarchivermodel "github.com/DggHQ/dggarchiver-model"
	"github.com/nats-io/nats.go"
	luaLibs "github.com/vadv/gopher-lua-libs"
	lua "github.com/yuin/gopher-lua"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrUnableToCreate = errors.New("unable to create batch job")

type K8s struct {
	k8sCfg    config.K8sConfig
	image     string
	natsHost  string
	natsTopic string
}

func New(cfg *config.Config) *K8s {
	return &K8s{
		k8sCfg:    cfg.Controller.K8s,
		image:     cfg.Controller.WorkerImage,
		natsHost:  cfg.NATS.Host,
		natsTopic: cfg.NATS.Topic,
	}
}

func (k *K8s) ListWorkers(_ context.Context) ([]orchestration.Worker, error) {
	// TODO: actually implement this
	return []orchestration.Worker{}, nil
}

func (k *K8s) StartWorker(_ context.Context, data []byte, vod *dggarchivermodel.VOD) error {
	var completions, parallelism, ttl, backoffLimit int32 = 1, 1, 30, 0
	jobName := fmt.Sprintf("dggarchiver-worker-%s", vod.ID)

	var livestreamURL string
	switch vod.Platform {
	case "youtube":
		livestreamURL = fmt.Sprintf("https://youtu.be/%s", vod.ID)
	case "rumble", "kick":
		livestreamURL = vod.PlaybackURL
	}

	jobs := k.k8sCfg.K8sClientSet.BatchV1().Jobs(k.k8sCfg.Namespace)
	jobSpec := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: k.k8sCfg.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Completions:             &completions,
			Parallelism:             &parallelism,
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					ImagePullSecrets: []v1.LocalObjectReference{
						{
							Name: "registry-1",
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "dggworker-volume",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: "dggworker-pvc",
								},
							},
						},
					},
					RestartPolicy: v1.RestartPolicyNever,
					Containers: []v1.Container{
						{
							Name:  jobName,
							Image: k.image,
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      "dggworker-volume",
									MountPath: "/videos",
								},
							},
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									v1.ResourceMemory: k.k8sCfg.MemoryQuantity,
									v1.ResourceCPU:    k.k8sCfg.CPUQuantity,
								},
							},
							Env: []v1.EnvVar{
								{
									Name:  "LIVESTREAM_INFO",
									Value: string(data),
								},
								{
									Name:  "LIVESTREAM_ID",
									Value: vod.ID,
								},
								{
									Name:  "LIVESTREAM_URL",
									Value: livestreamURL,
								},
								{
									Name:  "LIVESTREAM_DOWNLOADER",
									Value: vod.Downloader,
								},
								{
									Name:  "NATS_HOST",
									Value: k.natsHost,
								},
								{
									Name:  "NATS_TOPIC",
									Value: k.natsTopic,
								},
								{
									Name:  "VERBOSE",
									Value: "true",
								},
							},
						},
					},
				},
			},
		},
	}
	batch, err := jobs.Create(context.TODO(), jobSpec, metav1.CreateOptions{})
	if err != nil {
		return errors.Join(ErrUnableToCreate, err)
	}
	slog.Debug("batch created", slog.Group("batch", slog.String("name", batch.Name), slog.String("namespace", k.k8sCfg.Namespace)))

	return nil
}

func (k *K8s) Listen(ctx context.Context, cfg *config.Config) {
	L := lua.NewState()
	if cfg.Controller.Plugins.Enabled {
		luaLibs.Preload(L)
		if err := L.DoFile(cfg.Controller.Plugins.PathToPlugin); err != nil {
			slog.Error("unable to load lua script", slog.Any("err", err))
			os.Exit(1)
		}
	}

	if _, err := cfg.NATS.NatsConnection.Subscribe(fmt.Sprintf("%s.job", cfg.NATS.Topic), func(msg *nats.Msg) {
		vod := &dggarchivermodel.VOD{}
		if err := json.Unmarshal(msg.Data, vod); err != nil {
			slog.Error("unable to unmarshal VOD",
				slog.String("vod", string(msg.Data)),
				slog.Any("err", err),
			)
			return
		}

		slog.Info("VOD received", slog.Group("vod", slog.String("id", vod.ID), slog.String("platform", vod.Platform), slog.String("downloader", vod.Downloader)))

		if cfg.Controller.Plugins.Enabled {
			util.LuaCallReceiveFunction(L, vod)
		}

		if err := k.StartWorker(ctx, msg.Data, vod); err != nil {
			slog.Error("unable to create batch job", slog.Any("err", err))
			return
		}

		if cfg.Controller.Plugins.Enabled {
			util.LuaCallContainerFunction(L, vod, true)
		}
	}); err != nil {
		slog.Error("unable to subscribe to NATS topic", slog.Any("err", err))
		os.Exit(1)
	}
}
