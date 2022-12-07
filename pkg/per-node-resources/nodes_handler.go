package pernoderesources

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
)

type nodeStatus string

const (
	ready    nodeStatus = "Ready"
	notReady nodeStatus = "NotReady"
)

type nodeInfo struct {
	status            nodeStatus
	usedResources     map[string]corev1.ResourceList
	podInformerCancel context.CancelFunc
	podInformerReady  bool
	allocatable       corev1.ResourceList
}

// NodesHandler exposes method to handle resources divided per node and foreign cluster.
type NodesHandler struct {
	mutex   sync.RWMutex
	nodeMap map[string]nodeInfo
}

// NewNodesHandler creates and returns a new NodesHandler having an empty map of nodes.
func NewNodesHandler() *NodesHandler {
	return &NodesHandler{
		nodeMap: make(map[string]nodeInfo),
	}
}

func (nh *NodesHandler) setCancelFunctionFor(nodeName string, fn context.CancelFunc) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodeMap[nodeName]; exists {
		if !entry.podInformerReady {
			entry.podInformerCancel = fn
			nh.nodeMap[nodeName] = entry
		}
	}
}

func (nh *NodesHandler) setPodInformerReadyFor(nodeName string) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodeMap[nodeName]; exists {
		entry.podInformerReady = true
		nh.nodeMap[nodeName] = entry
	}
}

func (nh *NodesHandler) insertNewReadyNode(nodeName string, allocatable corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	node := nodeInfo{
		status:        ready,
		usedResources: make(map[string]corev1.ResourceList, 0),
		allocatable:   allocatable,
	}
	nh.nodeMap[nodeName] = node
}

func (nh *NodesHandler) turnNodeOff(nodeName string) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()
	if entry, exists := nh.nodeMap[nodeName]; exists {
		if entry.status == ready {
			entry.status = notReady
			nh.nodeMap[nodeName] = entry
		}
	}
}

func (nh *NodesHandler) addPodToNode(nodeName, clusterID string, resources corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	_, nodeExists := nh.nodeMap[nodeName]
	if !nodeExists {
		// it should never happen
		return
	}
	nodeInfo := nh.nodeMap[nodeName]
	if entry, exists := nodeInfo.usedResources[clusterID]; exists {
		addResources(entry, resources)
		nodeInfo.usedResources[clusterID] = entry
	}
}

func (nh *NodesHandler) deletePodFromNode(nodeName, clusterID string, resources corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	_, nodeExists := nh.nodeMap[nodeName]
	if !nodeExists {
		// it should never happen
		return
	}
	nodeInfo := nh.nodeMap[nodeName]
	if entry, exists := nodeInfo.usedResources[clusterID]; exists {
		subResources(entry, resources)
		nodeInfo.usedResources[clusterID] = entry
	}
}

func (nh *NodesHandler) decreaseAllocatableForNode(nodeName string, resources corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodeMap[nodeName]; exists {
		subResources(entry.allocatable, resources)
		nh.nodeMap[nodeName] = entry
	}
}

func (nh *NodesHandler) increaseAllocatableForNode(nodeName string, resources corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodeMap[nodeName]; exists {
		addResources(entry.allocatable, resources)
		nh.nodeMap[nodeName] = entry
	}
}

func (nh *NodesHandler) setAllocatableForNode(nodeName string, resources corev1.ResourceList) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	if entry, exists := nh.nodeMap[nodeName]; exists {
		entry.allocatable = resources
		nh.nodeMap[nodeName] = entry
	}
}

func (nh *NodesHandler) deleteClusterByClusterID(clusterID string) {
	nh.mutex.Lock()
	defer nh.mutex.Unlock()

	for _, entry := range nh.nodeMap {
		delete(entry.usedResources, clusterID)
	}
}

func (nh *NodesHandler) getWholeClusterAllocatable() corev1.ResourceList {
	nh.mutex.RLock()
	defer nh.mutex.RUnlock()

	resources := corev1.ResourceList{}

	for _, entry := range nh.nodeMap {
		if entry.status == ready && entry.podInformerReady {
			addResources(resources, entry.allocatable)
		}
	}

	return resources
}

func (nh *NodesHandler) getClusterIDResources(clusterID string) corev1.ResourceList {
	nh.mutex.RLock()
	defer nh.mutex.RUnlock()

	resources := corev1.ResourceList{}

	for _, nodeEntry := range nh.nodeMap {
		if nodeEntry.status == ready && nodeEntry.podInformerReady {
			if clusterResources, exists := nodeEntry.usedResources[clusterID]; exists {
				addResources(resources, clusterResources)
			}
		}
	}

	return resources
}
