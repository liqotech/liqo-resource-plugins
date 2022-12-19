package main

import (
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	grpcserver "github.com/liqotech/liqo-resource-plugins/pkg/per-node-resources"
	"github.com/liqotech/liqo/pkg/utils/restcfg"
)

func main() {
	var port int
	var selector string
	var resyncPeriod time.Duration
	var nodeSelector labels.Selector
	var includeVirtualNodes bool

	var rootCmd = &cobra.Command{
		Use:          os.Args[0],
		Short:        "Liqo plugin which devides the available resource among peered clusters",
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if nodeSelector, err = labels.Parse(selector); err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			config := restcfg.SetRateLimiter(ctrl.GetConfigOrDie())
			clientset := kubernetes.NewForConfigOrDie(config)

			return grpcserver.ListenAndServeGRPCServer(port, clientset, resyncPeriod, nodeSelector, includeVirtualNodes)
		},
	}

	rootCmd.PersistentFlags().BoolVar(&includeVirtualNodes, "include-virtual-nodes", false,
		"if true resources of virtual nodes are considered, otherwise ignored")
	rootCmd.PersistentFlags().DurationVar(&resyncPeriod, "resync-period", 10*time.Hour, "the resync period for the informers")
	rootCmd.PersistentFlags().IntVar(&port, "port", 6001, "set port where the server will listen on.")
	rootCmd.PersistentFlags().StringVar(&selector, "selector", "", "the selector to filter nodes. This flag can be used multiple times.")

	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
