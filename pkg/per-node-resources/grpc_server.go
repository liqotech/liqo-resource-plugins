// Copyright 2019-2022 The Liqo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pernoderesources

//TODO: setup klog and use it.
import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"

	resourcemonitors "github.com/liqotech/liqo/pkg/liqo-controller-manager/resource-request-controller/resource-monitors"
	"github.com/liqotech/liqo/pkg/utils"
	"github.com/liqotech/liqo/pkg/virtualKubelet/forge"
)

type server struct {
	Server *grpc.Server
	resourcemonitors.ResourceReaderServer
	subscribers       sync.Map
	allocatable       corev1.ResourceList
	nodesHandler      *NodesHandler
	createPodInformer func(nodeName string)
}

// ListenAndServeGRPCServer creates the gRPC server and makes it listen on the given port.
func ListenAndServeGRPCServer(port int, clientset *kubernetes.Clientset,
	resyncPeriod time.Duration, nodesToIgnore []string) error {
	ctx := ctrl.SetupSignalHandler()
	// server setup. The stream is not initialized here because it needs a subscriber so
	// it will be initialized in the Subscribe method below
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	s := server{
		Server:       grpc.NewServer(),
		allocatable:  corev1.ResourceList{},
		nodesHandler: NewNodesHandler(),
	}

	nodeFactory := informers.NewSharedInformerFactoryWithOptions(
		clientset, resyncPeriod, informers.WithTweakListOptions(noVirtualNodesFilter),
	)
	nodeInformer := nodeFactory.Core().V1().Nodes().Informer()

	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onNodeAdd,
		UpdateFunc: s.onNodeUpdate,
		DeleteFunc: s.onNodeDelete,
	})

	nodeFactory.Start(ctx.Done())
	nodeFactory.WaitForCacheSync(ctx.Done())

	s.createPodInformer = func(nodeName string) {
		informerCtx, cancel := context.WithCancel(ctx)
		podFactory := informers.NewSharedInformerFactoryWithOptions(
			clientset, resyncPeriod, informers.WithTweakListOptions(noShadowPodsFilter),
			informers.WithTweakListOptions(filterByNodeName(nodeName)),
		)
		podInformer := podFactory.Core().V1().Pods().Informer()
		podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: s.onPodAdd,
			// We do not care about update events, since resources are immutable.
			DeleteFunc: s.onPodDelete,
		})
		//TODO: is the cancel function still required?
		s.nodesHandler.setCancelFunctionFor(nodeName, cancel)
		podFactory.Start(informerCtx.Done())
		// wait to synch the cache before write the resource and notify
		podFactory.WaitForCacheSync(informerCtx.Done())
		s.nodesHandler.setPodInformerReadyFor(nodeName)
	}

	// register this server using the register interface defined in liqo
	resourcemonitors.RegisterResourceReaderServer(s.Server, &s)
	log.Printf("info: external resource monitor listening at %v", lis.Addr())
	if err := s.Server.Serve(lis); err != nil {
		return fmt.Errorf("grpc server failed to serve: %w", err)
	}

	return nil
}

// ReadResources receives a clusterID and returns the resources for that specific clusterID. In this version of the resource plugin
// the clusterID is ignored and the same resources are returned for every clusterID received. Since this method could be called multiple
// times it has to be idempotent.
func (s *server) ReadResources(ctx context.Context, req *resourcemonitors.ClusterIdentity) (*resourcemonitors.ResourceList, error) {
	log.Printf("info: reading resources for cluster %s", req.ClusterID)

	wholeClusterAllocatable := s.nodesHandler.getWholeClusterAllocatable()
	addResources(wholeClusterAllocatable, s.nodesHandler.getClusterIDResources(req.ClusterID))
	normalizeResources(wholeClusterAllocatable)

	return &resourcemonitors.ResourceList{Resources: mapResources(wholeClusterAllocatable)}, nil
}

// Subscribe is quite standard in this implementation so the only thing that it does is to notify liqo immediately.
func (s *server) Subscribe(req *resourcemonitors.Empty, srv resourcemonitors.ResourceReader_SubscribeServer) error {
	log.Printf("info: liqo controller manager subscribed")

	// Store the stream. Using req as key since each request will have a different req object.
	s.subscribers.Store(req, srv)
	ctx := srv.Context()

	for {
		<-ctx.Done()
		s.subscribers.Delete(req)
		log.Printf("info: liqo controller manager disconnected")
		return nil
	}
}

// NotifyChange uses the cached streams to notify liqo that some resources changed. This method receives a clusterID inside req
// which can be a real clusterID or resourcemonitors.AllClusterIDs which tells to liqo to refresh all the resources
// of all the peered clusters.
func (s *server) NotifyChange(ctx context.Context, req *resourcemonitors.ClusterIdentity) error {
	log.Printf("info: sending notification to liqo controller manager for cluster %q", req.ClusterID)
	var err error

	s.subscribers.Range(func(key, value interface{}) bool {
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

	log.Printf("info: notification sent to liqo controller manager for cluster %q", req.ClusterID)
	return err
}

func (s *server) NotifyAll() error {
	err := s.NotifyChange(context.Background(), &resourcemonitors.ClusterIdentity{ClusterID: resourcemonitors.AllClusterIDs})
	if err != nil {
		return err
	}
	return nil
}

// RemoveCluster is useful to clean cluster's information if it exists when a cluster is upeered. This method receives
// a clusterID which identifies the cluster that has been removed. We believe that this method is useful in custom
// implementation, for example where a database is involved in the implementation.
func (s *server) RemoveCluster(ctx context.Context, req *resourcemonitors.ClusterIdentity) (*resourcemonitors.Empty, error) {
	log.Printf("info: removing cluster having clusterID %s", req.ClusterID)

	s.nodesHandler.deleteClusterByClusterID(req.ClusterID)
	return &resourcemonitors.Empty{}, nil
}

// react to a Node Creation/First informer run.
func (s *server) onNodeAdd(obj interface{}) {
	node := obj.(*corev1.Node)
	go s.createPodInformer(node.Name)
	if utils.IsNodeReady(node) {
		log.Printf("Adding Node %s", node.Name)
		s.nodesHandler.insertNewReadyNode(node.Name, node.Status.Allocatable)
		err := s.NotifyAll()
		if err != nil {
			log.Printf("error: error during notifying a change")
		}
	}
}

// react to a Node Update.
func (s *server) onNodeUpdate(oldObj, newObj interface{}) {
	oldNode := oldObj.(*corev1.Node)
	newNode := newObj.(*corev1.Node)
	newNodeResources := newNode.Status.Allocatable
	log.Printf("Updating Node %s", oldNode.Name)
	if utils.IsNodeReady(newNode) {
		// node was already Ready, update with possible new resources.
		if utils.IsNodeReady(oldNode) {
			s.nodesHandler.setAllocatableForNode(oldNode.Name, newNodeResources)
		} else {
			s.nodesHandler.insertNewReadyNode(newNode.Name, newNodeResources)
			go s.createPodInformer(newNode.Name)
		}
		// node is terminating or stopping, delete all its resources.
	} else if utils.IsNodeReady(oldNode) && !utils.IsNodeReady(newNode) {
		s.nodesHandler.turnNodeOff(oldNode.Name)
	}
	err := s.NotifyAll()
	if err != nil {
		log.Printf("error: error during notifying a change")
	}
}

// react to a Node Delete.
func (s *server) onNodeDelete(obj interface{}) {
	node := obj.(*corev1.Node)
	if utils.IsNodeReady(node) {
		log.Printf("info: Deleting Node %s", node.Name)
		s.nodesHandler.turnNodeOff(node.Name)
	}
	err := s.NotifyAll()
	if err != nil {
		log.Printf("error: error during notifying a change")
	}
}

func (s *server) onPodAdd(obj interface{}) {
	// Thanks to the filters at the informer level, add events are received only when pods running on physical nodes turn running.
	podAdded := obj.(*corev1.Pod)
	log.Printf("info: OnPodAdd: Add for pod %s:%s", podAdded.Namespace, podAdded.Name)

	podResources := extractPodResources(podAdded)
	s.nodesHandler.decreaseAllocatableForNode(podAdded.Spec.NodeName, podResources)

	if clusterID := podAdded.Labels[forge.LiqoOriginClusterIDKey]; clusterID != "" {
		log.Printf("info: OnPodAdd: Pod %s:%s passed ClusterID check. ClusterID = %s", podAdded.Namespace, podAdded.Name, clusterID)
		s.nodesHandler.addPodToNode(podAdded.Spec.NodeName, clusterID, podResources)
	}
	err := s.NotifyAll()
	if err != nil {
		log.Printf("error: error during notifying a change")
	}
}

func (s *server) onPodDelete(obj interface{}) {
	// Thanks to the filters at the informer level, delete events are received only when
	// pods previously running on a physical node are no longer running.
	podDeleted := obj.(*corev1.Pod)
	log.Printf("info: OnPodDelete: Delete for pod %s:%s", podDeleted.Namespace, podDeleted.Name)

	podResources := extractPodResources(podDeleted)
	s.nodesHandler.increaseAllocatableForNode(podDeleted.Spec.NodeName, podResources)

	if clusterID := podDeleted.Labels[forge.LiqoOriginClusterIDKey]; clusterID != "" {
		log.Printf("info: OnPodDelete: Pod %s:%s passed ClusterID check. ClusterID = %s", podDeleted.Namespace, podDeleted.Name, clusterID)
		s.nodesHandler.deletePodFromNode(podDeleted.Spec.NodeName, clusterID, podResources)
	}
	err := s.NotifyAll()
	if err != nil {
		log.Printf("error: error during notifying a change")
	}
}
