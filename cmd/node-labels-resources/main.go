package main

import (
	goflags "flag"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	grpcserver "github.com/liqotech/liqo-resource-plugins/pkg/node-labels-resources"
	monitorargs "github.com/liqotech/liqo-resource-plugins/pkg/utils/args"
	clients "github.com/liqotech/liqo-resource-plugins/pkg/utils/clients"
)

func main() {
	fs := goflags.NewFlagSet("", goflags.PanicOnError)
	klog.InitFlags(fs)
	klog.SetOutput(os.Stdout)
	var port int
	nodeLabels := monitorargs.NodeLabelsMap{}
	kubernetesClient, err := clients.CreateKubernetesCore()
	if err != nil {
		klog.Fatalf("error: unable to create kubernetes client: %s", err)
	}
	var rootCmd = &cobra.Command{
		Use:          os.Args[0],
		Short:        "Liqo plugin which provides resources based on node selector to each remote cluster",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return grpcserver.ListenAndServeGRPCServer(port, nodeLabels.NodeLabels, kubernetesClient)
		},
	}

	rootCmd.Flags().Var(&nodeLabels, "node-label", "set a node label having format name=value. e.g.: --node-label=label1=v1 --node-label=label2=v2.")
	rootCmd.PersistentFlags().IntVar(&port, "port", 6001, "set port where the server will listen on.")
	rootCmd.Flags().AddGoFlagSet(fs)

	err = rootCmd.Root().MarkFlagRequired("node-label")
	if err != nil {
		klog.Fatalf("error: error during marking resource flag as required: %s", err)
	}
	err = rootCmd.Execute()
	if err != nil {
		klog.Flush()
		os.Exit(1)
	}
	klog.Flush()
}
