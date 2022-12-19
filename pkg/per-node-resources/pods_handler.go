package pernoderesources

import (
	"context"
	"log"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	pluginsutils "github.com/liqotech/liqo-resource-plugins/pkg/utils"
	"github.com/liqotech/liqo/pkg/virtualKubelet/forge"
)

// PodsHandler exposes methods to handle pods' resources divided per clusterID.
type PodsHandler struct {
	pods         map[string]corev1.ResourceList
	stopFunction context.CancelFunc
	cacheReady   bool
	mutex        sync.RWMutex
	nodesHandler *NodesHandler
	notifier     *server
}

// NewPodsHandler creates a new pods handler.
func NewPodsHandler(ctx context.Context, nodeName string, clientset *kubernetes.Clientset,
	resyncPeriod time.Duration, nodesHandler *NodesHandler, notifier *server) (*PodsHandler, error) {
	ph := &PodsHandler{
		pods:         make(map[string]corev1.ResourceList),
		cacheReady:   false,
		nodesHandler: nodesHandler,
		notifier:     notifier,
	}

	informerCtx, cancel := context.WithCancel(ctx)
	podFactory := informers.NewSharedInformerFactoryWithOptions(
		clientset, resyncPeriod,
		informers.WithTweakListOptions(pluginsutils.FilterPods(nodeName)),
	)
	podInformer := podFactory.Core().V1().Pods().Informer()
	_, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: ph.onPodAdd,
		// We do not care about update events, since resources are immutable.
		DeleteFunc: ph.onPodDelete,
	})
	if err != nil {
		cancel()
		return nil, err
	}

	ph.stopFunction = cancel
	podFactory.Start(informerCtx.Done())
	go func() {
		// wait to synch the cache before write the resource and notify
		podFactory.WaitForCacheSync(informerCtx.Done())
		if informerCtx.Err() == nil {
			ph.cacheReady = true
			ph.notifier.Notify()
		}
	}()

	return ph, nil
}

// onPodAdd reacts on pod add.
func (ph *PodsHandler) onPodAdd(obj interface{}) {
	// Thanks to the filters at the informer level, add events are received only when pods running on physical nodes turn running.
	podAdded := obj.(*corev1.Pod)
	log.Printf("info: OnPodAdd: Add for pod %s:%s", podAdded.Namespace, podAdded.Name)

	podResources := pluginsutils.ExtractPodResources(podAdded)
	ph.nodesHandler.decreaseAllocatableForNode(podAdded.Spec.NodeName, podResources)

	if clusterID := podAdded.Labels[forge.LiqoOriginClusterIDKey]; clusterID != "" {
		log.Printf("info: OnPodAdd: Pod %s:%s passed ClusterID check. ClusterID = %s", podAdded.Namespace, podAdded.Name, clusterID)
		ph.addPod(clusterID, podResources)
	}
	ph.notifier.Notify()
}

// onPodDelete reacts on pod delete.
func (ph *PodsHandler) onPodDelete(obj interface{}) {
	// Thanks to the filters at the informer level, delete events are received only when
	// pods previously running on a physical node are no longer running.
	podDeleted := obj.(*corev1.Pod)
	log.Printf("info: OnPodDelete: Delete for pod %s:%s", podDeleted.Namespace, podDeleted.Name)

	podResources := pluginsutils.ExtractPodResources(podDeleted)
	ph.nodesHandler.increaseAllocatableForNode(podDeleted.Spec.NodeName, podResources)

	if clusterID := podDeleted.Labels[forge.LiqoOriginClusterIDKey]; clusterID != "" {
		log.Printf("info: OnPodDelete: Pod %s:%s passed ClusterID check. ClusterID = %s", podDeleted.Namespace, podDeleted.Name, clusterID)
		ph.deletePod(clusterID, podResources)
	}
	ph.notifier.Notify()
}

// IsCacheReady returns true if the cache is ready, false otherwise.
func (ph *PodsHandler) IsCacheReady() bool {
	return ph.cacheReady
}

// deletePod delete pod resources from the actual resource count.
func (ph *PodsHandler) deletePod(clusterID string, resources corev1.ResourceList) {
	ph.mutex.Lock()
	defer ph.mutex.Unlock()

	if entry, exists := ph.pods[clusterID]; exists {
		pluginsutils.SubResources(entry, resources)
		ph.pods[clusterID] = entry
	}
}

// addPod adds pod resources to the actual resource count.
func (ph *PodsHandler) addPod(clusterID string, resources corev1.ResourceList) {
	ph.mutex.Lock()
	defer ph.mutex.Unlock()

	if entry, exists := ph.pods[clusterID]; exists {
		pluginsutils.AddResources(entry, resources)
		ph.pods[clusterID] = entry
	}
}

// deletePodsByClusterID deletes all the pods scheduled from the cluster identified by the given clusterID.
func (ph *PodsHandler) deletePodsByClusterID(clusterID string) {
	ph.mutex.Lock()
	defer ph.mutex.Unlock()

	delete(ph.pods, clusterID)
}

// getPodsResourcesByClusterID returns all the resources used by the cluster idetified by the given clusterID.
func (ph *PodsHandler) getPodsResourcesByClusterID(clusterID string) corev1.ResourceList {
	if ph.cacheReady {
		ph.mutex.RLock()
		defer ph.mutex.RUnlock()

		if entry, exists := ph.pods[clusterID]; exists {
			return entry
		}
	}

	return corev1.ResourceList{}
}
