package main

import (
	"flag"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	grpcserver "github.com/liqotech/liqo-resource-plugins/pkg/per-node-resources"
	"github.com/liqotech/liqo/pkg/utils/restcfg"
)

func main() {
	var port int
	var nodesToIgnore []string

	var rootCmd = &cobra.Command{
		Use:          os.Args[0],
		Short:        "Liqo plugin which provides fixed resources to each remote cluster",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			resyncPeriod := flag.Duration("resync-period", 10*time.Hour, "The resync period for the informers")
			config := restcfg.SetRateLimiter(ctrl.GetConfigOrDie())
			clientset := kubernetes.NewForConfigOrDie(config)

			return grpcserver.ListenAndServeGRPCServer(port, clientset, *resyncPeriod, nodesToIgnore)
		},
	}

	rootCmd.PersistentFlags().IntVar(&port, "port", 6001, "set port where the server will listen on.")
	rootCmd.PersistentFlags().StringSliceVar(&nodesToIgnore, "ignore-node",
		make([]string, 0), "node's name to ignore. You can use this flag multiple times.")

	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
