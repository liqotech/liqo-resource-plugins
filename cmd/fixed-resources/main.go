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

package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"

	grpcserver "github.com/liqotech/liqo-resource-plugins/pkg/fixed-resources"
	monitorargs "github.com/liqotech/liqo-resource-plugins/pkg/utils/args"
)

func main() {
	var port int
	resources := monitorargs.QuantityMap{}

	var rootCmd = &cobra.Command{
		Use:          os.Args[0],
		Short:        "Liqo plugin which provides fixed resources to each remote cluster",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return grpcserver.ListenAndServeGRPCServer(port, resources.ResourceMap)
		},
	}

	rootCmd.Flags().Var(&resources, "resource", "set a resource having format name=quantity. e.g.: --resource=cpu=1000m --resource=memory=1G.")
	rootCmd.PersistentFlags().IntVar(&port, "port", 6001, "set port where the server will listen on.")

	err := rootCmd.Root().MarkFlagRequired("resource")
	if err != nil {
		log.Fatalf("error: error during marking resource flag as required: %s", err)
	}
	err = rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
