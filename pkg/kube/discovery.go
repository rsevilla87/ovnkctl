package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type OVNPod struct {
	Name      string
	Namespace string
	NodeName  string
	PodIP     string
	Ready     bool
}

type OVNTopology struct {
	Namespace         string
	NodePods          []OVNPod
	ControlPlanePods  []OVNPod
	NBDBContainer     string
	SBDBContainer     string
}

func DiscoverOVN(ctx context.Context, clientset kubernetes.Interface) (*OVNTopology, error) {
	topo := &OVNTopology{
		NBDBContainer: "nbdb",
		SBDBContainer: "sbdb",
	}

	nodePods, err := findOVNPods(ctx, clientset, "app=ovnkube-node")
	if err != nil {
		return nil, fmt.Errorf("finding ovnkube-node pods: %w", err)
	}
	if len(nodePods) == 0 {
		return nil, fmt.Errorf("no ovnkube-node pods found in any namespace")
	}
	topo.Namespace = nodePods[0].Namespace
	topo.NodePods = nodePods

	cpPods, err := findOVNPods(ctx, clientset, "app=ovnkube-control-plane")
	if err != nil {
		return nil, fmt.Errorf("finding ovnkube-control-plane pods: %w", err)
	}
	topo.ControlPlanePods = cpPods

	return topo, nil
}

func findOVNPods(ctx context.Context, clientset kubernetes.Interface, labelSelector string) ([]OVNPod, error) {
	podList, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	var pods []OVNPod
	for _, p := range podList.Items {
		pods = append(pods, OVNPod{
			Name:      p.Name,
			Namespace: p.Namespace,
			NodeName:  p.Spec.NodeName,
			PodIP:     p.Status.PodIP,
			Ready:     isPodReady(&p),
		})
	}
	return pods, nil
}

func isPodReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
