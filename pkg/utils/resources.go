package utils

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"
)

// AddResources is a utility function to add resources.
func AddResources(currentResources, toAdd corev1.ResourceList) {
	for resourceName, quantity := range toAdd {
		if value, exists := currentResources[resourceName]; exists {
			value.Add(quantity)
			currentResources[resourceName] = value
		} else {
			currentResources[resourceName] = quantity
		}
	}
}

// SubResources is an utility function to subtract resources.
func SubResources(currentResources, toSub corev1.ResourceList) {
	for resourceName, quantity := range toSub {
		if value, exists := currentResources[resourceName]; exists {
			value.Sub(quantity)
			currentResources[resourceName] = value
		}
	}
}

// ExtractPodResources get resources of a given pod.
func ExtractPodResources(podToExtract *corev1.Pod) corev1.ResourceList {
	resourcesToExtract, _ := resourcehelper.PodRequestsAndLimits(podToExtract)
	return resourcesToExtract
}

// NormalizeResources sets resource to 0 if it is negative.
func NormalizeResources(currentResources corev1.ResourceList) {
	for resourceName, value := range currentResources {
		if value.Sign() == -1 {
			value.Set(0)
			currentResources[resourceName] = value
		}
	}
}

// MapResources gets a resourceList and returns anoter map having a string key and a pointer to quantity as value type.
func MapResources(resourceList map[corev1.ResourceName]resource.Quantity) map[string]*resource.Quantity {
	result := make(map[string]*resource.Quantity, len(resourceList))

	for entry := range resourceList {
		resourceQuantity := resourceList[entry]
		result[entry.String()] = &resourceQuantity
	}

	return result
}
