# dggarchiver-controller
This is the controller service of the dggarchiver that starts worker containers. 

## Features

1. Supported orcherstration backends:
   - Docker
   - Kubernetes
2. Lua plugin support

## Lua

The service can be extended with Lua plugins/scripts. An example can be found in the ```controller.example.lua``` file.

If enabled, the service will call these functions from the specified ```.lua``` file:
- ```OnReceive(vod)``` when a job has been received, where ```vod``` is the livestream struct
- ```OnContainer(vod, success)``` when a worker container has been created, where ```vod``` is the livestream struct, and ```success``` is the boolean signifying success.

After the functions are done executing, the service will check the global ```ReceiveResponse``` and ```ContainerResponse``` variables for errors, before returning the struct. The struct's fields are:
```go
type LuaResponse struct {
	Filled  bool
	Error   bool
	Message string
	Data    map[string]interface{}
}
```

## Configuration

The config file location can be set with the ```CONFIG``` environment variable. You can also set the Docker API version with the ```DOCKER_API_VERSION``` environment variable. Example configuration can be found below and in the ```config.example.yaml``` file.

```yaml
controller:
  worker_image: ghcr.io/dgghq/dggarchiver-worker:main # mandatory field, sets the worker image used to download the livestreams
  docker:
    autoremove: yes # automatically remove the worker container after it's done, can be set to 'no' for debugging
    enabled: yes
    network: dggarchiver-network # mandatory field, the docker network to assign to the worker image
  k8s:
    enabled: no
    namespace: dgghq # mandatory field, sets the worker namespace
    cpu_limit: 150m # mandatory field, sets the worker cpu limit for K8s
    memory_limit: 50Mi # mandatory field, sets the worker memory limit for K8s
  plugins:
    enabled: no
    path: controller.lua # path to the lua plugin
  verbose: no # increases log verbosity
```