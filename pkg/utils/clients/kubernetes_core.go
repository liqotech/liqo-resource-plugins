package client

import (
	"log"
	"path/filepath"

	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// CreateKubernetesCore create Kuberentes client.
func CreateKubernetesCore() (*kubernetes.Clientset, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Print("k8s client failed in-cluster mode now defaulting to in-local mode")
		kubeconfig := filepath.Join(
			homedir.HomeDir(), ".kube", "config",
		)
		restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize the RestConfig")
	}
	restConfig.QPS = float32(50)
	restConfig.Burst = 50
	clientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize kubernetes client set")
	}
	return clientSet, nil
}
