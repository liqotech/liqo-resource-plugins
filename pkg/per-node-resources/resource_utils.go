package pernoderesources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"

	"github.com/liqotech/liqo/pkg/consts"
)

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

// extractPodResources get resources of a given pod.
func extractPodResources(podToExtract *corev1.Pod) corev1.ResourceList {
	resourcesToExtract, _ := resourcehelper.PodRequestsAndLimits(podToExtract)
	return resourcesToExtract
}

// checkSign checks if all the resources in a resource list are positive, otherwise returns an error.
func normalizeResources(currentResources corev1.ResourceList) {
	for resourceName, value := range currentResources {
		if value.Sign() == -1 {
			value.Set(0)
			currentResources[resourceName] = value
		}
	}
}

// noVirtualNodesFilter is used to filter and ignore virtual nodes at informer level.
func noVirtualNodesFilter(options *metav1.ListOptions) {
	req, err := labels.NewRequirement(consts.TypeLabel, selection.NotEquals, []string{consts.TypeNode})
	utilruntime.Must(err)
	options.LabelSelector = labels.NewSelector().Add(*req).String()
}

//TODO: create a function to filter control planes.

// noShadowPodsFilter is used to filter and ignore shadow pods at informer level.
func noShadowPodsFilter(options *metav1.ListOptions) {
	req, err := labels.NewRequirement(consts.LocalPodLabelKey, selection.NotEquals, []string{consts.LocalPodLabelValue})
	utilruntime.Must(err)
	options.LabelSelector = labels.NewSelector().Add(*req).String()
	options.FieldSelector = fields.OneTermEqualSelector("status.phase", string(corev1.PodRunning)).String()
}

// filterByNodeName creates a filter for a given node name.
func filterByNodeName(nodeName string) func(*metav1.ListOptions) {
	return func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	}
}

// mapResources gets a resourceList and returns anoter map having a string key and a pointer to quantity as value type.
func mapResources(resourceList map[corev1.ResourceName]resource.Quantity) map[string]*resource.Quantity {
	result := make(map[string]*resource.Quantity, len(resourceList))

	for entry := range resourceList {
		resourceQuantity := resourceList[entry]
		result[entry.String()] = &resourceQuantity
	}

	return result
}
