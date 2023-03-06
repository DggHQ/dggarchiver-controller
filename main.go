package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DggHQ/dggarchiver-controller/config"
	"github.com/DggHQ/dggarchiver-controller/util"
	log "github.com/DggHQ/dggarchiver-logger"
	dggarchivermodel "github.com/DggHQ/dggarchiver-model"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/rabbitmq/amqp091-go"
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
	cfg.Initialize()

	if cfg.Flags.Verbose {
		log.SetLevel(log.DebugLevel)
	}

	msgs, err := cfg.AMQPConfig.Channel.Consume(
		cfg.AMQPConfig.QueueName, // queue
		"controller",             // consumer
		true,                     // auto-ack
		false,                    // exclusive
		false,                    // no-local
		false,                    // no-wait
		nil,                      // args
	)
	if err != nil {
		log.Fatalf("Wasn't able to start consuming the %s queue: %s", cfg.AMQPConfig.QueueName, err)
	}

	if cfg.UseK8s {
		log.Infof("%s", "Running in Kubernetes Mode.")
		go k8sBatchWorker(&cfg, msgs, ctx)
	} else {
		log.Infof("%s", "Running in Docker Mode.")
		go dockerWorker(&cfg, msgs, ctx)
	}

	log.Infof("Waiting for VODs...")
	var forever chan struct{}
	<-forever
}

func dockerWorker(cfg *config.Config, msgs <-chan amqp091.Delivery, ctx context.Context) {
	L := lua.NewState()
	defer L.Close()
	if cfg.PluginConfig.On {
		luaLibs.Preload(L)
		if err := L.DoFile(cfg.PluginConfig.PathToScript); err != nil {
			log.Fatalf("Wasn't able to load the Lua script: %s", err)
		}
	} else {
		L.Close()
	}
	for d := range msgs {
		vod := &dggarchivermodel.YTVod{}
		err := json.Unmarshal(d.Body, vod)
		if err != nil {
			log.Errorf("Wasn't able to unmarshal VOD, skipping: %s", err)
			continue
		}
		log.Infof("Received a VOD: %s", vod)
		if cfg.PluginConfig.On {
			util.LuaCallReceiveFunction(L, vod)
		}

		containerName := fmt.Sprintf("dggarchiver-worker-%s", vod.ID)

		container, err := cfg.DockerConfig.DockerSocket.ContainerCreate(ctx, &container.Config{
			Image: "dgghq/dggarchiver-worker:latest",
			Env: []string{
				fmt.Sprintf("LIVESTREAM_INFO=%s", d.Body),
				fmt.Sprintf("LIVESTREAM_ID=%s", vod.ID),
				fmt.Sprintf("AMQP_URI=%s", cfg.AMQPConfig.URI),
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
			AutoRemove: true,
		}, &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				cfg.DockerConfig.NetworkName: {
					NetworkID: cfg.DockerConfig.NetworkName,
				},
			},
		}, nil, containerName)
		if err != nil {
			log.Errorf("Wasn't able to create the worker container, skipping: %s", err)
			continue
		}

		if err := cfg.DockerConfig.DockerSocket.ContainerStart(ctx, container.ID, types.ContainerStartOptions{}); err != nil {
			log.Errorf("Wasn't able to start the worker container, skipping: %s", err)
			continue
		}

		if cfg.PluginConfig.On {
			util.LuaCallContainerFunction(L, vod, err == nil)
		}
	}
}

func k8sBatchWorker(cfg *config.Config, msgs <-chan amqp091.Delivery, ctx context.Context) {
	L := lua.NewState()
	defer L.Close()
	if cfg.PluginConfig.On {
		luaLibs.Preload(L)
		if err := L.DoFile(cfg.PluginConfig.PathToScript); err != nil {
			log.Fatalf("Wasn't able to load the Lua script: %s", err)
		}
	} else {
		L.Close()
	}
	for d := range msgs {
		vod := &dggarchivermodel.YTVod{}
		err := json.Unmarshal(d.Body, vod)
		if err != nil {
			log.Errorf("Wasn't able to unmarshal VOD, skipping: %s", err)
			continue
		}
		log.Infof("Received a VOD: %s", vod)
		if cfg.PluginConfig.On {
			util.LuaCallReceiveFunction(L, vod)
		}

		jobName := fmt.Sprintf("dggarchiver-worker-%s", vod.ID)
		var completions, parallelism, ttl, backoffLimit int32 = 1, 1, 30, 0
		clientSet := cfg.K8sConfig.K8sClientSet
		jobs := clientSet.BatchV1().Jobs(cfg.K8sConfig.Namespace)
		jobSpec := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: cfg.K8sConfig.Namespace,
			},
			Spec: batchv1.JobSpec{
				BackoffLimit:            &backoffLimit,
				TTLSecondsAfterFinished: &ttl,
				Completions:             &completions,
				Parallelism:             &parallelism,
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
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
								Image: "dgghq/dggarchiver-worker:latest",
								VolumeMounts: []v1.VolumeMount{
									{
										Name:      "dggworker-volume",
										MountPath: "/videos",
									},
								},
								Env: []v1.EnvVar{
									{
										Name:  "LIVESTREAM_INFO",
										Value: string(d.Body),
									},
									{
										Name:  "LIVESTREAM_ID",
										Value: vod.ID,
									},
									{
										Name:  "AMQP_URI",
										Value: cfg.AMQPConfig.URI,
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
		log.Infof("Batch '%s' created in namespace '%s'.\n", batch.Name, cfg.K8sConfig.Namespace)

		if cfg.PluginConfig.On {
			util.LuaCallContainerFunction(L, vod, err == nil)
		}
	}
}
