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

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	pluginsutils "github.com/liqotech/liqo-resource-plugins/pkg/utils"
	resourcemonitors "github.com/liqotech/liqo/pkg/liqo-controller-manager/resource-request-controller/resource-monitors"
)

type server struct {
	Server *grpc.Server
	resourcemonitors.ResourceReaderServer
	subscribers  sync.Map
	nodesHandler *NodesHandler
	notifyChan   chan bool
}

// ListenAndServeGRPCServer creates the gRPC server and makes it listen on the given port.
func ListenAndServeGRPCServer(port int, clientset *kubernetes.Clientset,
	resyncPeriod time.Duration, nodeSelector labels.Selector, includeVirtualNodes bool) error {
	ctx := ctrl.SetupSignalHandler()
	// server setup. The stream is not initialized here because it needs a subscriber so
	// it will be initialized in the Subscribe method below
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	s := server{
		Server:     grpc.NewServer(),
		notifyChan: make(chan bool, 1),
	}
	nh, err := NewNodesHandler(ctx, clientset, resyncPeriod, &s, nodeSelector, includeVirtualNodes)
	if err != nil {
		return err
	}
	s.nodesHandler = nh

	// This is a custom implementation, it waits for pods informers cache to be ready
	nh.WaitForCacheSync()

	// register this server using the register interface defined in liqo
	resourcemonitors.RegisterResourceReaderServer(s.Server, &s)
	log.Printf("info: external resource monitor listening at %v", lis.Addr())
	go s.notifier()
	log.Printf("info: notifier started")
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
	pluginsutils.AddResources(wholeClusterAllocatable, s.nodesHandler.getClusterIDResources(req.ClusterID))
	pluginsutils.NormalizeResources(wholeClusterAllocatable)

	return &resourcemonitors.ResourceList{Resources: pluginsutils.MapResources(wholeClusterAllocatable)}, nil
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

// RemoveCluster is useful to clean cluster's information if it exists when a cluster is upeered. This method receives
// a clusterID which identifies the cluster that has been removed. We believe that this method is useful in custom
// implementation, for example where a database is involved in the implementation.
func (s *server) RemoveCluster(ctx context.Context, req *resourcemonitors.ClusterIdentity) (*resourcemonitors.Empty, error) {
	log.Printf("info: removing cluster having clusterID %s", req.ClusterID)

	s.nodesHandler.deletePodsByClusterID(req.ClusterID)
	return &resourcemonitors.Empty{}, nil
}

// Notify uses the cached streams to notify liqo that some resources changed.
// It notifies Liqo every second if an event that requires a resources update occurred.
func (s *server) notifier() {
	log.Printf("info: sending notification to liqo controller manager for all clusters")
	var err error
	for {
		<-s.notifyChan
		s.subscribers.Range(func(key, value interface{}) bool {
			stream := value.(resourcemonitors.ResourceReader_SubscribeServer)

			err = stream.Send(&resourcemonitors.ClusterIdentity{ClusterID: resourcemonitors.AllClusterIDs})
			if err != nil {
				err = fmt.Errorf("error: error during sending a notification %w", err)
			}
			return true
		})
		if err != nil {
			fmt.Printf("%s", err)
		}

		log.Printf("info: notification sent to liqo controller manager for all clusters")
		time.Sleep(time.Second)
	}
}

// Notify send a message in the notifyChannel to let the notifier know that it must send a notification.
func (s *server) Notify() {
	select {
	case s.notifyChan <- false:
	default:
	}
}
