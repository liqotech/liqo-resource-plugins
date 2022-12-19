package utils

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/liqotech/liqo/pkg/utils"
)

const (
	controlPlane = "node-role.kubernetes.io/master"
	master       = "node-role.kubernetes.io/control-plane"
)

// IsNodeReadyAndSchedulable return true if the given node is ready and schedulable, false otherwise.
func IsNodeReadyAndSchedulable(node *corev1.Node) bool {
	return utils.IsNodeReady(node) && isSchedulable(node.Spec.Taints)
}

func isSchedulable(taints []corev1.Taint) bool {
	for _, taint := range taints {
		if (taint.Key == controlPlane || taint.Key == master) && taint.Effect == corev1.TaintEffectNoSchedule {
			return false
		}
	}

	return true
}
