package config

import (
	"context"
	"os"
	"strings"

	log "github.com/DggHQ/dggarchiver-logger"
	docker "github.com/docker/docker/client"
	"github.com/joho/godotenv"
	amqp "github.com/rabbitmq/amqp091-go"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Flags struct {
	Verbose bool
}

type AMQPConfig struct {
	URI          string
	ExchangeName string
	ExchangeType string
	QueueName    string
	Context      context.Context
	Channel      *amqp.Channel
	connection   *amqp.Connection
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
	AMQPConfig   AMQPConfig
	DockerConfig DockerConfig
	PluginConfig PluginConfig
	K8sConfig    K8sConfig
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

	// Flags
	verbose := strings.ToLower(os.Getenv("VERBOSE"))
	if verbose == "1" || verbose == "true" {
		cfg.Flags.Verbose = true
	}

	// AMQP
	cfg.AMQPConfig.URI = os.Getenv("AMQP_URI")
	if cfg.AMQPConfig.URI == "" {
		log.Fatalf("Please set the AMQP_URI environment variable and restart the app")
	}
	cfg.AMQPConfig.ExchangeName = ""
	cfg.AMQPConfig.ExchangeType = "direct"
	cfg.AMQPConfig.QueueName = "notifier"

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

func (cfg *Config) loadAMQP() {
	var err error

	cfg.AMQPConfig.Context = context.Background()

	cfg.AMQPConfig.connection, err = amqp.Dial(cfg.AMQPConfig.URI)
	if err != nil {
		log.Fatalf("Wasn't able to connect to the AMQP server: %s", err)
	}

	cfg.AMQPConfig.Channel, err = cfg.AMQPConfig.connection.Channel()
	if err != nil {
		log.Fatalf("Wasn't able to create the AMQP channel: %s", err)
	}

	_, err = cfg.AMQPConfig.Channel.QueueDeclare(
		cfg.AMQPConfig.QueueName, // queue name
		true,                     // durable
		false,                    // auto delete
		false,                    // exclusive
		false,                    // no wait
		nil,                      // arguments
	)
	if err != nil {
		log.Fatalf("Wasn't able to declare the AMQP queue: %s", err)
	}
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
	cfg.loadAMQP()
	if cfg.UseK8s {
		cfg.loadK8sConfig()
	} else {
		cfg.loadDocker()
	}
}
