# Liqo resource plugins

⚠️ This Project is Deprecated ⚠️

Please note: This repository is no longer actively maintained and is considered deprecated. We encourage you to explore alternative solutions or repositories that are actively developed.

------

This repository contains resource plugins that you can use to customize the amount of resources that [Liqo](https://github.com/liqotech/liqo) offers to each foreign cluster.
You can either use the plugin that fits your use case or decide to start from an existing one and extend/modify it according to your needs. **Resource plugins are supported in Liqo v0.6.0 and above**.

## Plugins

### Fixed resources (fixed-resources)

This plugin provides a fixed quantity of resources for each peered cluster.
You can configure the number of resources that this plugin yields directly in the [deployment.yaml](./deploy/fixed-resources/deployment.yaml) by adding/modifying some arguments:

```yaml
    args:
    - --resource=cpu=2000m
    - --resource=memory=2G
    - --resource=pods=10
    - --resource=storage=10G
    - --resource=ephemeral-storage=10G
```

The resources that you specify in the args must respect the [standard Kubernetes resources format](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#resource-units-in-kubernetes).

You can also set a subset of the resources above but remember always to set at least `cpu`, `memory` and `pod`, otherwise the consumer cluster will not be able to offload any pods.

You can change the listening port of the server by using an additional arg `--port=<PORT_NUMBER>`.

## Usage

You can deploy the selected plugin through:

```bash
kubectl apply -f ./deploy/<PLUGIN_NAME> -n liqo
```

Remember to update the containerPort in the proper service.yaml if you changed the server's listening port.

PLUGIN_NAME is the name that you can see enclosed by brackets, e.g. fixed-resources.

Now that you've deployed the plugin you should configure Liqo to use it.
You can use again the installation command with an extra flag:

```bash
liqoctl install ... --set controllerManager.config.resourcePluginAddress=liqo-fixed-resources-plugin.liqo:6001
```

You may need to update resource offers only if the variation of resources is significant for you.
Liqo exposes `controllerManager.config.offerUpdateThresholdPercentage` as Helm variable that enables this behavior so you don't have to care about writing this functionality inside your external resource monitor.
You can read more about this parameter in the [Liqo helm documentation](https://github.com/liqotech/liqo/tree/master/deployments/liqo).

## How to create your plugin

First of all, clone this repository and perform the following steps:

- Create a folder inside [cmd](./cmd/) named as your plugin, e.g. fixed-resources.
- Create a `main.go` file inside the folder you've just created. This file will contain the needed logic to bootstrap your plugin and make it run.
- Create a folder inside [pkg](./pkg/) named as your plugin, e.g. fixed-resources. The name of this folder must match the name of the first folder you've created. You will develop your plugin in this folder.
- Create a folder inside [deploy](./deploy/) with the same name as the previous folders and put here the yaml files to deploy your plugin in the Kubernetes cluster.

The plugin must implement an already existing interface that contains the following methods:

- **ReadResources**: receives a clusterID and returns the resources for the specific cluster identified by the clusterID. Since this method could be called multiple times it has to be idempotent.

- **Subscribe**: this method is called by the Liqo controller manager when it starts and it needs to open a stream.
It is important to remember that this method must never return otherwise the opened stream will be dropped and the subscription ended.

- **RemoveCluster**: RemoveCluster is useful to clean cluster's information when a cluster is unpeered. This method receives a clusterID which identifies the cluster that has been removed.

Existing plugins within this repo provide an example about the implementation of the above functions.

If you've done everything correctly you will be able to build your plugin and deploy it. To build your plugin you need to create a docker image by running the following command:

```bash
docker build -f ./build/common/Dockerfile -t <IMAGE_NAME>:<TAG> --build-arg PLUGIN=<PLUGIN_NAME> .
```

You must replace `<PLUGIN_NAME>` with the name of the folders that you've created during the plugin's scaffolding.

Now you can use the docker image to deploy the plugin to your Kubernetes cluster. The simplest way to use the image is to push it to a docker repository and refer to the image in the `deployment.yaml` file.
