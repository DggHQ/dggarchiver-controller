package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	config "github.com/DggHQ/dggarchiver-config/controller"
	"github.com/DggHQ/dggarchiver-controller/util"
	log "github.com/DggHQ/dggarchiver-logger"
	dggarchivermodel "github.com/DggHQ/dggarchiver-model"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/nats-io/nats.go"
	luaLibs "github.com/vadv/gopher-lua-libs"
	lua "github.com/yuin/gopher-lua"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	if cfg.Controller.K8s.Enabled {
		log.Infof("%s", "Running in Kubernetes Mode.")
		go k8sBatchWorker(&cfg, ctx)
	} else {
		log.Infof("%s", "Running in Docker Mode.")
		go dockerWorker(&cfg, ctx)
	}

	log.Infof("Waiting for VODs...")
	var forever chan struct{}
	<-forever
}

func dockerWorker(cfg *config.Config, ctx context.Context) {
	L := lua.NewState()
	defer L.Close()
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
		}
		log.Infof("Received a VOD: %s", vod)
		if cfg.Controller.Plugins.Enabled {
			util.LuaCallReceiveFunction(L, vod)
		}
		containerName := fmt.Sprintf("dggarchiver-worker-%s", vod.ID)

		var livestreamUrl string
		switch vod.Platform {
		case "youtube":
			livestreamUrl = fmt.Sprintf("https://youtu.be/%s", vod.ID)
		case "rumble", "kick":
			livestreamUrl = vod.PlaybackURL
		}

		container, err := cfg.Controller.Docker.DockerSocket.ContainerCreate(ctx, &container.Config{
			Image: cfg.Controller.WorkerImage,
			Env: []string{
				fmt.Sprintf("LIVESTREAM_INFO=%s", msg.Data),
				fmt.Sprintf("LIVESTREAM_ID=%s", vod.ID),
				fmt.Sprintf("LIVESTREAM_URL=%s", livestreamUrl),
				fmt.Sprintf("LIVESTREAM_PLATFORM=%s", vod.Platform),
				fmt.Sprintf("LIVESTREAM_DOWNLOADER=%s", vod.Downloader),
				fmt.Sprintf("NATS_HOST=%s", cfg.NATS.Host),
				fmt.Sprintf("NATS_TOPIC=%s", cfg.NATS.Topic),
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
			AutoRemove: cfg.Controller.Docker.AutoRemove,
		}, &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				cfg.Controller.Docker.Network: {
					NetworkID: cfg.Controller.Docker.Network,
				},
			},
		}, nil, containerName)
		if err != nil {
			log.Errorf("Wasn't able to create the worker container, skipping: %s", err)
		}

		if err := cfg.Controller.Docker.DockerSocket.ContainerStart(ctx, container.ID, types.ContainerStartOptions{}); err != nil {
			log.Errorf("Wasn't able to start the worker container, skipping: %s", err)
		}

		if cfg.Controller.Plugins.Enabled {
			util.LuaCallContainerFunction(L, vod, err == nil)
		}

	}); err != nil {
		log.Fatalf("An error occured when subscribing to topic: %s", err)
	}
}

func k8sBatchWorker(cfg *config.Config, ctx context.Context) {
	L := lua.NewState()
	defer L.Close()
	if cfg.Controller.Plugins.Enabled {
		luaLibs.Preload(L)
		if err := L.DoFile(cfg.Controller.Plugins.PathToPlugin); err != nil {
			log.Fatalf("Wasn't able to load the Lua script: %s", err)
		}
	}

	if _, err := cfg.NATS.NatsConnection.Subscribe(fmt.Sprintf("%s.job", cfg.NATS.Topic), func(msg *nats.Msg) {
		vod := &dggarchivermodel.VOD{}
		err := json.Unmarshal(msg.Data, vod)
		if err != nil {
			log.Errorf("Wasn't able to unmarshal VOD, skipping: %s", err)
		}
		log.Infof("Received a VOD: %s", vod)
		if cfg.Controller.Plugins.Enabled {
			util.LuaCallReceiveFunction(L, vod)
		}

		var livestreamUrl string
		switch vod.Platform {
		case "youtube":
			livestreamUrl = fmt.Sprintf("https://youtu.be/%s", vod.ID)
		case "rumble", "kick":
			livestreamUrl = vod.PlaybackURL
		}

		jobName := fmt.Sprintf("dggarchiver-worker-%s", vod.ID)
		var completions, parallelism, ttl, backoffLimit int32 = 1, 1, 30, 0
		clientSet := cfg.Controller.K8s.K8sClientSet
		jobs := clientSet.BatchV1().Jobs(cfg.Controller.K8s.Namespace)
		jobSpec := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: cfg.Controller.K8s.Namespace,
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
								Image: cfg.Controller.WorkerImage,
								VolumeMounts: []v1.VolumeMount{
									{
										Name:      "dggworker-volume",
										MountPath: "/videos",
									},
								},
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										v1.ResourceMemory: cfg.Controller.K8s.MemoryQuantity,
										v1.ResourceCPU:    cfg.Controller.K8s.CPUQuantity,
									},
								},
								Env: []v1.EnvVar{
									{
										Name:  "LIVESTREAM_INFO",
										Value: string(msg.Data),
									},
									{
										Name:  "LIVESTREAM_ID",
										Value: vod.ID,
									},
									{
										Name:  "LIVESTREAM_URL",
										Value: livestreamUrl,
									},
									{
										Name:  "LIVESTREAM_DOWNLOADER",
										Value: vod.Downloader,
									},
									{
										Name:  "NATS_HOST",
										Value: cfg.NATS.Host,
									},
									{
										Name:  "NATS_TOPIC",
										Value: cfg.NATS.Topic,
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
			log.Fatalf("Error creating batch job: %s", err)
		}
		log.Infof("Batch '%s' created in namespace '%s'.\n", batch.Name, cfg.Controller.K8s.Namespace)

		if cfg.Controller.Plugins.Enabled {
			util.LuaCallContainerFunction(L, vod, err == nil)
		}
	}); err != nil {
		log.Fatalf("An error occured when subscribing to topic: %s", err)
	}
}
