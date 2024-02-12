package nodeselectorresources

import (
	"context"
	"fmt"
	"log"
	"net"
	"reflect"
	"sync"
	"time"

	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/liqotech/liqo/pkg/consts"
	resourcemonitors "github.com/liqotech/liqo/pkg/liqo-controller-manager/resource-request-controller/resource-monitors"
	"github.com/liqotech/liqo/pkg/utils"
)

// NodeDetails stores details of the Node.
type NodeDetails struct {
	Schedulable bool
	Allocatable corev1.ResourceList
}

type nodeLabelsMonitor struct {
	Server *grpc.Server
	resourcemonitors.ResourceReaderServer
	subscribers   sync.Map
	nodeLabels    map[string]string
	k8sNodeClient v1.NodeInterface
	allocatable   corev1.ResourceList
	nodeMutex     sync.RWMutex
	ctx           context.Context
	resourceLists map[string]NodeDetails
}

// ListenAndServeGRPCServer creates the gRPC server and makes it listen on the given port.
func ListenAndServeGRPCServer(port int, nodeLabels map[string]string, clientset *kubernetes.Clientset) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	resyncPeriod := 10 * time.Hour
	ctx := ctrl.SetupSignalHandler()
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	// this function is used to filter and ignore virtual nodes at informer level.
	var noVirtualNodesFilter = func(options *metav1.ListOptions) {
		labelSelector := labels.NewSelector()
		for k, v := range nodeLabels {
			req, err := labels.NewRequirement(k, selection.Equals, []string{v})
			utilruntime.Must(err)
			labelSelector = labelSelector.Add(*req)
		}
		req, err := labels.NewRequirement(consts.TypeLabel, selection.NotEquals, []string{consts.TypeNode})
		utilruntime.Must(err)
		labelSelector = labelSelector.Add(*req)
		klog.V(1).Infof("Node selector resource monitor label selector: %s", labelSelector.String())
		options.LabelSelector = labelSelector.String()
	}

	nodeFactory := informers.NewSharedInformerFactoryWithOptions(
		clientset, resyncPeriod, informers.WithTweakListOptions(noVirtualNodesFilter),
	)
	nodeInformer := nodeFactory.Core().V1().Nodes().Informer()

	s := nodeLabelsMonitor{
		Server:        grpc.NewServer(),
		nodeLabels:    nodeLabels,
		allocatable:   corev1.ResourceList{},
		k8sNodeClient: clientset.CoreV1().Nodes(),
		ctx:           ctx,
		resourceLists: map[string]NodeDetails{},
	}
	_, err = nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onNodeAdd,
		UpdateFunc: s.onNodeUpdate,
		DeleteFunc: s.onNodeDelete,
	})
	utilruntime.Must(err)
	nodeFactory.Start(ctx.Done())
	nodeFactory.WaitForCacheSync(ctx.Done())

	resourcemonitors.RegisterResourceReaderServer(s.Server, &s)
	if err := s.Server.Serve(lis); err != nil {
		return fmt.Errorf("grpc server failed to serve: %w", err)
	}
	log.Printf("info: node selector resource monitor listening at %v", lis.Addr())
	return nil
}

// react to a Node Creation/First informer run.
func (nlm *nodeLabelsMonitor) onNodeAdd(obj interface{}) {
	node := obj.(*corev1.Node)
	toAdd := &node.Status.Allocatable
	klog.V(4).Infof("Adding Node %s", node.Name)
	nlm.resourceLists[node.Name] = NodeDetails{
		Allocatable: *toAdd,
		Schedulable: utils.IsNodeReady(node) && !node.Spec.Unschedulable,
	}
	nlm.writeClusterResources()
}

// react to a Node Update.
func (nlm *nodeLabelsMonitor) onNodeUpdate(oldObj, newObj interface{}) {
	oldNode := oldObj.(*corev1.Node)
	newNode := newObj.(*corev1.Node)
	newNodeResources := newNode.Status.Allocatable
	klog.V(4).Infof("Updating Node %s", oldNode.Name)
	if utils.IsNodeReady(newNode) && !newNode.Spec.Unschedulable {
		nodeDetail, ok := nlm.resourceLists[newNode.Name]
		if !ok {
			nlm.resourceLists[newNode.Name] = NodeDetails{
				Allocatable: newNodeResources,
				Schedulable: true,
			}
		} else {
			nodeDetail.Schedulable = true
			nlm.resourceLists[newNode.Name] = nodeDetail
		}
	} else if !utils.IsNodeReady(newNode) || newNode.Spec.Unschedulable {
		nodeDetail, ok := nlm.resourceLists[newNode.Name]
		if ok {
			klog.V(4).Infof("Marking Node %s as Unscheduable", newNode.Name)
			nodeDetail.Schedulable = false
			nlm.resourceLists[newNode.Name] = nodeDetail
		}
	}
	nlm.writeClusterResources()
}

// react to a Node Delete.
func (nlm *nodeLabelsMonitor) onNodeDelete(obj interface{}) {
	node := obj.(*corev1.Node)
	toDelete := &node.Status.Allocatable
	nodeResourceList, ok := nlm.resourceLists[node.Name]
	if ok {
		if !reflect.DeepEqual(nodeResourceList, *toDelete) {
			klog.Warningf("Node %s resources changed while it was terminating, ignoring", node.Name)
		}
		klog.V(4).Infof("Deleting Node %s", node.Name)
		delete(nlm.resourceLists, node.Name)
	}
	nlm.writeClusterResources()
}

func (nlm *nodeLabelsMonitor) writeClusterResources() {
	nodeAllocatable := corev1.ResourceList{}
	for _, nodeDetail := range nlm.resourceLists {
		if !nodeDetail.Schedulable {
			continue
		}
		addResources(nodeAllocatable, nodeDetail.Allocatable)
	}
	nlm.nodeMutex.Lock()
	nlm.allocatable = nodeAllocatable.DeepCopy()
	klog.V(4).Infof("Cluster resources: %v", nlm.allocatable)
	nlm.nodeMutex.Unlock()
	err := nlm.NotifyChange(nlm.ctx, &resourcemonitors.ClusterIdentity{ClusterID: resourcemonitors.AllClusterIDs})
	if err != nil {
		log.Printf("error: error during sending notification to liqo: %s", err)
	}
}

// ReadResources receives a clusterID and returns the resources for that specific clusterID. In this version of the resource plugin
// the clusterID is ignored and the same resources are returned for every clusterID received. Since this method could be called multiple
// times it has to be idempotent.
func (nlm *nodeLabelsMonitor) ReadResources(ctx context.Context, req *resourcemonitors.ClusterIdentity) (*resourcemonitors.PoolResourceList, error) {
	klog.V(4).Infof("info: reading resources for cluster %s", req.ClusterID)
	nlm.nodeMutex.RLock()
	defer nlm.nodeMutex.RUnlock()
	allocatable := make(map[string]*resource.Quantity)
	for resName, quantity := range nlm.allocatable {
		val := quantity.DeepCopy()
		allocatable[resName.String()] = &val
	}
	klog.V(4).Infof("Cluster resources: %v", allocatable)

	resourceList := []*resourcemonitors.ResourceList{{Resources: allocatable}}
	return &resourcemonitors.PoolResourceList{ResourceLists: resourceList}, nil
}

// Subscribe is quite standard in this implementation so the only thing that it does is to notify liqo immediately.
func (nlm *nodeLabelsMonitor) Subscribe(req *resourcemonitors.Empty, srv resourcemonitors.ResourceReader_SubscribeServer) error {
	klog.V(1).Infof("info: liqo controller manager subscribed")

	// Store the stream. Using req as key since each request will have a different req object.
	nlm.subscribers.Store(req, srv)
	ctx := srv.Context()

	// This notification is useful since you can edit the resources declared in the deployment and apply it to the cluster when one or more
	// foreign clusters are already peered so this broadcast notification will update the resources for those clusters.
	err := nlm.NotifyChange(ctx, &resourcemonitors.ClusterIdentity{ClusterID: resourcemonitors.AllClusterIDs})
	if err != nil {
		klog.V(1).Infof("error: error during sending notification to liqo: %s", err)
	}

	<-ctx.Done()
	nlm.subscribers.Delete(req)
	klog.V(1).Infof("info: liqo controller manager disconnected")
	return nil
}

// NotifyChange uses the cached streams to notify liqo that some resources changed. This method receives a clusterID inside req
// which can be a real clusterID or resourcemonitors.AllClusterIDs which tells to liqo to refresh all the resources
// of all the peered clusters.
func (nlm *nodeLabelsMonitor) NotifyChange(ctx context.Context, req *resourcemonitors.ClusterIdentity) error {
	klog.V(1).Infof("info: sending notification to liqo controller manager for cluster %q", req.ClusterID)
	var err error

	nlm.subscribers.Range(func(key, value interface{}) bool {
		stream := value.(resourcemonitors.ResourceReader_SubscribeServer)

		err = stream.Send(req)
		if err != nil {
			err = fmt.Errorf("error: error during sending a notification %w", err)
		}
		return true
	})
	if err != nil {
		fmt.Printf("%s", err)
		return err
	}

	klog.V(1).Infof("info: notification sent to liqo controller manager for cluster %q", req.ClusterID)
	return err
}

// RemoveCluster is useful to clean cluster's information if it exists when a cluster is upeered. This method receives
// a clusterID which identifies the cluster that has been removed. We believe that this method is useful in custom
// implementation, for example where a database is involved in the implementation.
func (nlm *nodeLabelsMonitor) RemoveCluster(ctx context.Context, req *resourcemonitors.ClusterIdentity) (*resourcemonitors.Empty, error) {
	klog.V(1).Infof("info: removing cluster %s", req.ClusterID)
	return &resourcemonitors.Empty{}, nil
}

// addResources is a utility function to add resources.
func addResources(currentResources, toAdd corev1.ResourceList) {
	for resourceName, quantity := range toAdd {
		if value, exists := currentResources[resourceName]; exists {
			value.Add(quantity)
			currentResources[resourceName] = value
		} else {
			currentResources[resourceName] = quantity
		}
	}
}

// subResources is an utility function to subtract resources.
func subResources(currentResources, toSub corev1.ResourceList) {
	for resourceName, quantity := range toSub {
		if value, exists := currentResources[resourceName]; exists {
			value.Sub(quantity)
			currentResources[resourceName] = value
		}
	}
}
