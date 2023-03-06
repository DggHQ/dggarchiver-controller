package config

import (
	"os"
	"strings"
	"time"

	log "github.com/DggHQ/dggarchiver-logger"
	docker "github.com/docker/docker/client"
	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Flags struct {
	Verbose bool
}

type NATSConfig struct {
	Host           string
	Topic          string
	NatsConnection *nats.Conn
}

type DockerConfig struct {
	NetworkName  string
	DockerSocket *docker.Client
}

type K8sConfig struct {
	K8sClientSet *kubernetes.Clientset
	Namespace    string
}

type PluginConfig struct {
	On           bool
	PathToScript string
}

type Config struct {
	UseK8s       bool
	Flags        Flags
	DockerConfig DockerConfig
	PluginConfig PluginConfig
	K8sConfig    K8sConfig
	NATSConfig   NATSConfig
}

func (cfg *Config) loadDotEnv() {
	log.Debugf("Loading environment variables")
	godotenv.Load()

	// Decide whether k8s should be used as an orchestration backend
	// 1 or true will load the k8s cluster config
	// Additionally the namespace will be configures from env when using k8s
	usek8s := strings.ToLower(os.Getenv("USEK8S"))
	if usek8s == "1" || usek8s == "true" {
		cfg.UseK8s = true
		// Set the K8s Namespace from env only if k8s is used
		cfg.K8sConfig.Namespace = os.Getenv("K8S_NAMESPACE")
		if cfg.K8sConfig.Namespace == "" {
			log.Fatalf("Please set K8S_NAMESPACE when using K8s as a container orcherstration backend")
		}
	} else {
		cfg.UseK8s = false
	}

	// NATS Host Name or IP
	cfg.NATSConfig.Host = os.Getenv("NATS_HOST")
	if cfg.NATSConfig.Host == "" {
		log.Fatalf("Please set the NATS_HOST environment variable and restart the app")
	}

	// NATS Topic Naem
	cfg.NATSConfig.Topic = os.Getenv("NATS_TOPIC")
	if cfg.NATSConfig.Topic == "" {
		log.Fatalf("Please set the NATS_TOPIC environment variable and restart the app")
	}

	// Flags
	verbose := strings.ToLower(os.Getenv("VERBOSE"))
	if verbose == "1" || verbose == "true" {
		cfg.Flags.Verbose = true
	}

	// Docker
	cfg.DockerConfig.NetworkName = os.Getenv("DOCKER_NETWORK")
	if cfg.DockerConfig.NetworkName == "" {
		log.Fatalf("Please set the DOCKER_NETWORK environment variable and restart the app")
	}

	// Lua Plugins
	plugins := strings.ToLower(os.Getenv("PLUGINS"))
	if plugins == "1" || plugins == "true" {
		cfg.PluginConfig.On = true
		cfg.PluginConfig.PathToScript = os.Getenv("LUA_PATH_TO_SCRIPT")
		if cfg.PluginConfig.PathToScript == "" {
			log.Fatalf("Please set the LUA_PATH_TO_SCRIPT environment variable and restart the app")
		}
	}

	log.Debugf("Environment variables loaded successfully")
}

func (cfg *Config) loadNats() {
	// Connect to NATS server
	nc, err := nats.Connect(cfg.NATSConfig.Host, nil, nats.PingInterval(20*time.Second), nats.MaxPingsOutstanding(5))
	if err != nil {
		log.Fatalf("Could not connect to NATS server: %s", err)
	}
	log.Infof("Successfully connected to NATS server: %s", cfg.NATSConfig.Host)
	cfg.NATSConfig.NatsConnection = nc
}

func (cfg *Config) loadDocker() {
	var err error

	cfg.DockerConfig.DockerSocket, err = docker.NewClientWithOpts(docker.FromEnv)
	if err != nil {
		log.Fatalf("Wasn't able to connect to the docker socket: %s", err)
	}
}

func (cfg *Config) loadK8sConfig() {
	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Could not get k8s cluster config: %s", err)
	}
	clientSet, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		log.Fatalf("Could not create new client set config: %s", err)
	}
	cfg.K8sConfig.K8sClientSet = clientSet
}

func (cfg *Config) Initialize() {
	cfg.loadDotEnv()
	cfg.loadNats()
	if cfg.UseK8s {
		cfg.loadK8sConfig()
	} else {
		cfg.loadDocker()
	}
}
