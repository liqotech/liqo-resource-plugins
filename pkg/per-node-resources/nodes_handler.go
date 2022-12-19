package pernoderesources

import (
	"context"
	"log"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	pluginsutils "github.com/liqotech/liqo-resource-plugins/pkg/utils"
)

type nodeInfo struct {
	ready       bool
	podsHandler *PodsHandler
	allocatable corev1.ResourceList
}

// NodesHandler exposes methods to handle resources divided per node.
type NodesHandler struct {
	mutex             sync.RWMutex
	notifier          *server
	nodes             map[string]nodeInfo
	initChan          chan bool
	createPodInformer func(string) *PodsHandler
}

// NewNodesHandler creates and returns a new NodesHandler having an empty map of nodes.
func NewNodesHandler(ctx context.Context, clientset *kubernetes.Clientset, resyncPeriod time.Duration,
	notifier *server, nodeSelector labels.Selector, includeVirtualNodes bool) (*NodesHandler, error) {
	nh := &NodesHandler{
		notifier: notifier,
		nodes:    make(map[string]nodeInfo),
		initChan: make(chan bool, 1),
	}

	nh.createPodInformer = func(nodeName string) *PodsHandler {
		ph, err := NewPodsHandler(ctx, nodeName, clientset, resyncPeriod, nh, notifier)
		if err != nil {
			//TODO: what should we do? If the pod handler creations fails nothing will work.
			return nil
		}

		nh.mutex.Lock()
		defer nh.mutex.Unlock()

		if entry, exists := nh.nodes[nodeName]; exists {
			entry.podsHandler = ph
			nh.nodes[nodeName] = entry
		}

		select {
		case nh.initChan <- false:
		default:
		}

		return ph
	}

	nodeFactory := informers.NewSharedInformerFactoryWithOptions(
		clientset, resyncPeriod, informers.WithTweakListOptions(pluginsutils.FilterNodes(nodeSelector, includeVirtualNodes)),
	)
	nodeInformer := nodeFactory.Core().V1().Nodes().Informer()

	_, err := nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    nh.onNodeAdd,
		UpdateFunc: nh.onNodeUpdate,
		DeleteFunc: nh.onNodeDelete,
	})
	if err != nil {
		return nil, err
	}

	nodeFactory.Start(ctx.Done())
	nodeFactory.WaitForCacheSync(ctx.Done())

	return nh, nil
}

// insertNewNode creates a nodeInfo struct to store all node's information.
func (nh *NodesHandler) insertNewNode(nodeName string, allocatable corev1.ResourceList, nodeReady bool) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	node := nodeInfo{
		podsHandler: nh.createPodInformer(nodeName),
		allocatable: allocatable,
		ready:       nodeReady,
	}
	nh.nodes[nodeName] = node
}

// turnNodeOff toggle node status from ready to not-ready and vice-versa.
func (nh *NodesHandler) turnNodeOff(nodeName string) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodes[nodeName]; exists {
		if entry.ready {
			entry.ready = false
			nh.nodes[nodeName] = entry
		}
	}
}

// decreaseAllocatableForNode subtracts the given resources from the node's allocatable resources.
func (nh *NodesHandler) decreaseAllocatableForNode(nodeName string, resources corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodes[nodeName]; exists {
		pluginsutils.SubResources(entry.allocatable, resources)
		nh.nodes[nodeName] = entry
	}
}

// increaseAllocatableForNode adds the given resources to the node's allocatable resources.
func (nh *NodesHandler) increaseAllocatableForNode(nodeName string, resources corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodes[nodeName]; exists {
		pluginsutils.AddResources(entry.allocatable, resources)
		nh.nodes[nodeName] = entry
	}
}

// setAllocatableForNode substitutes the actual resources with the given ones.
func (nh *NodesHandler) setAllocatableForNode(nodeName string, resources corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodes[nodeName]; exists {
		entry.allocatable = resources
		nh.nodes[nodeName] = entry
	}
}

// deletePodsByClusterID deletes all the pods belonging to the cluster identified by the given clusterID.
func (nh *NodesHandler) deletePodsByClusterID(clusterID string) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	for _, entry := range nh.nodes {
		entry.podsHandler.deletePodsByClusterID(clusterID)
	}
}

func (nh *NodesHandler) getWholeClusterAllocatable() corev1.ResourceList {
	nh.mutex.RLock()
	defer nh.mutex.RUnlock()

	resources := corev1.ResourceList{}

	for _, entry := range nh.nodes {
		if entry.ready && entry.podsHandler.cacheReady {
			pluginsutils.AddResources(resources, entry.allocatable)
		}
	}

	return resources
}

func (nh *NodesHandler) getClusterIDResources(clusterID string) corev1.ResourceList {
	nh.mutex.RLock()
	defer nh.mutex.RUnlock()

	resources := corev1.ResourceList{}

	for _, nodeEntry := range nh.nodes {
		if nodeEntry.ready {
			pluginsutils.AddResources(resources, nodeEntry.podsHandler.getPodsResourcesByClusterID(clusterID))
		}
	}

	return resources
}

// react to a Node Creation/First informer run.
func (nh *NodesHandler) onNodeAdd(obj interface{}) {
	node := obj.(*corev1.Node)
	log.Printf("Adding Node %s", node.Name)
	nh.insertNewNode(node.Name, node.Status.Allocatable, pluginsutils.IsNodeReadyAndSchedulable(node))
}

// react to a Node Update.
func (nh *NodesHandler) onNodeUpdate(oldObj, newObj interface{}) {
	oldNode := oldObj.(*corev1.Node)
	newNode := newObj.(*corev1.Node)
	newNodeResources := newNode.Status.Allocatable
	log.Printf("Updating Node %s", oldNode.Name)
	if pluginsutils.IsNodeReadyAndSchedulable(newNode) {
		// node was already Ready, update with possible new resources.
		// if ready but not schedulable it doesn't update the resources since it is useless.
		// the resources will be updated as soon as the node becomes schedulable.
		if pluginsutils.IsNodeReadyAndSchedulable(oldNode) {
			nh.setAllocatableForNode(oldNode.Name, newNodeResources)
			nh.notifier.Notify()
		} else {
			nh.insertNewNode(newNode.Name, newNodeResources, false)
		}
		// node is terminating or stopping, delete all its resources.
	} else if pluginsutils.IsNodeReadyAndSchedulable(oldNode) && !pluginsutils.IsNodeReadyAndSchedulable(newNode) {
		nh.turnNodeOff(oldNode.Name)
		nh.notifier.Notify()
	}
}

// react to a Node Delete.
func (nh *NodesHandler) onNodeDelete(obj interface{}) {
	node := obj.(*corev1.Node)
	log.Printf("info: Deleting Node %s", node.Name)
	if entry, exists := nh.nodes[node.Name]; exists {
		entry.podsHandler.stopFunction()
	}
	delete(nh.nodes, node.Name)
	nh.notifier.Notify()
}

func (nh *NodesHandler) areAllPodsInformerReady() bool {
	nh.mutex.RLock()
	defer nh.mutex.RUnlock()

	for _, entry := range nh.nodes {
		if !entry.podsHandler.cacheReady {
			return false
		}
	}
	return true
}

// WaitForCacheSync waits for pods informers cache to be ready.
func (nh *NodesHandler) WaitForCacheSync() {
	for {
		<-nh.initChan
		if nh.areAllPodsInformerReady() {
			break
		}
	}
}
