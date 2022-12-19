package utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/liqotech/liqo/pkg/consts"
)

// FilterNodes is used to filter and ignore virtual nodes at informer level.
func FilterNodes(nodeSelector labels.Selector, includeVirtualNodes bool) func(*metav1.ListOptions) {
	return func(options *metav1.ListOptions) {
		if !includeVirtualNodes {
			req, err := labels.NewRequirement(consts.TypeLabel, selection.NotEquals, []string{consts.TypeNode})
			utilruntime.Must(err)
			nodeSelector = nodeSelector.Add(*req)
		}
		options.LabelSelector = nodeSelector.String()
	}
}

// FilterPods creates a filter for a given node name.
func FilterPods(nodeName string) func(*metav1.ListOptions) {
	return func(options *metav1.ListOptions) {
		options.FieldSelector = fields.AndSelectors(fields.OneTermEqualSelector("spec.nodeName", nodeName),
			fields.OneTermEqualSelector("status.phase", string(corev1.PodRunning))).String()
	}
}
