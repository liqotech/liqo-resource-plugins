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

package fixedresources

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/api/resource"

	resourcemonitors "github.com/liqotech/liqo/pkg/liqo-controller-manager/resource-request-controller/resource-monitors"
)

type server struct {
	Server *grpc.Server
	resourcemonitors.ResourceReaderServer
	subscribers sync.Map
	resources   map[string]*resource.Quantity
}

// ListenAndServeGRPCServer creates the gRPC server and makes it listen on the given port.
func ListenAndServeGRPCServer(port int, resources map[string]*resource.Quantity) error {
	// server setup. The stream is not initialized here because it needs a subscriber so
	// it will be initialized in the Subscribe method below
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	s := server{
		Server:    grpc.NewServer(),
		resources: resources,
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
func (a *server) ReadResources(ctx context.Context, req *resourcemonitors.ClusterIdentity) (*resourcemonitors.PoolResourceList, error) {
	log.Printf("info: reading resources for cluster %s", req.ClusterID)
	resourceList := []*resourcemonitors.ResourceList{{Resources: a.resources}}
	response := resourcemonitors.PoolResourceList{ResourceLists: resourceList}
	return &response, nil
}

// Subscribe is quite standard in this implementation so the only thing that it does is to notify liqo immediately.
func (a *server) Subscribe(req *resourcemonitors.Empty, srv resourcemonitors.ResourceReader_SubscribeServer) error {
	log.Printf("info: liqo controller manager subscribed")

	// Store the stream. Using req as key since each request will have a different req object.
	a.subscribers.Store(req, srv)
	ctx := srv.Context()

	// This notification is useful since you can edit the resources declared in the deployment and apply it to the cluster when one or more
	// foreign clusters are already peered so this broadcast notification will update the resources for those clusters.
	err := a.NotifyChange(ctx, &resourcemonitors.ClusterIdentity{ClusterID: resourcemonitors.AllClusterIDs})
	if err != nil {
		log.Printf("error: error during sending notification to liqo: %s", err)
	}

	for {
		<-ctx.Done()
		a.subscribers.Delete(req)
		log.Printf("info: liqo controller manager disconnected")
		return nil
	}
}

// NotifyChange uses the cached streams to notify liqo that some resources changed. This method receives a clusterID inside req
// which can be a real clusterID or resourcemonitors.AllClusterIDs which tells to liqo to refresh all the resources
// of all the peered clusters.
func (a *server) NotifyChange(ctx context.Context, req *resourcemonitors.ClusterIdentity) error {
	log.Printf("info: sending notification to liqo controller manager for cluster %q", req.ClusterID)
	var err error

	a.subscribers.Range(func(key, value interface{}) bool {
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

// RemoveCluster is useful to clean cluster's information if it exists when a cluster is upeered. This method receives
// a clusterID which identifies the cluster that has been removed. We believe that this method is useful in custom
// implementation, for example where a database is involved in the implementation.
func (a *server) RemoveCluster(ctx context.Context, req *resourcemonitors.ClusterIdentity) (*resourcemonitors.Empty, error) {
	log.Printf("info: removing cluster having clusterID %s", req.ClusterID)
	return &resourcemonitors.Empty{}, nil
}
