package ovn

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/rsevilla/ovnkctl/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type Client struct {
	kubeClient *kube.Client
	topology   *kube.OVNTopology
	targetNode string
}

func NewClient(kubeClient *kube.Client, topology *kube.OVNTopology) *Client {
	return &Client{
		kubeClient: kubeClient,
		topology:   topology,
	}
}

func (c *Client) NBCtl(ctx context.Context, args ...string) (string, error) {
	pod, err := c.resolveTargetPod()
	if err != nil {
		return "", err
	}
	cmdArgs := append([]string{"ovn-nbctl"}, args...)
	return c.ExecInPod(ctx, pod.Namespace, pod.Name, c.topology.NBDBContainer, cmdArgs)
}

func (c *Client) NBCtlOnNode(ctx context.Context, nodeName string, args ...string) (string, error) {
	pod, err := c.findNodePod(nodeName)
	if err != nil {
		return "", err
	}
	cmdArgs := append([]string{"ovn-nbctl"}, args...)
	return c.ExecInPod(ctx, pod.Namespace, pod.Name, c.topology.NBDBContainer, cmdArgs)
}

func (c *Client) SBCtl(ctx context.Context, args ...string) (string, error) {
	pod, err := c.resolveTargetPod()
	if err != nil {
		return "", err
	}
	cmdArgs := append([]string{"ovn-sbctl"}, args...)
	return c.ExecInPod(ctx, pod.Namespace, pod.Name, c.topology.SBDBContainer, cmdArgs)
}

func (c *Client) SBCtlOnNode(ctx context.Context, nodeName string, args ...string) (string, error) {
	pod, err := c.findNodePod(nodeName)
	if err != nil {
		return "", err
	}
	cmdArgs := append([]string{"ovn-sbctl"}, args...)
	return c.ExecInPod(ctx, pod.Namespace, pod.Name, c.topology.SBDBContainer, cmdArgs)
}

func (c *Client) OVSCtl(ctx context.Context, nodeName string, args ...string) (string, error) {
	pod, err := c.findNodePod(nodeName)
	if err != nil {
		return "", err
	}
	cmdArgs := append([]string{"ovs-vsctl"}, args...)
	return c.ExecInPod(ctx, pod.Namespace, pod.Name, "ovnkube-controller", cmdArgs)
}

func (c *Client) AppCtl(ctx context.Context, container string, args ...string) (string, error) {
	pod, err := c.resolveTargetPod()
	if err != nil {
		return "", err
	}
	cmdArgs := append([]string{"ovn-appctl"}, args...)
	return c.ExecInPod(ctx, pod.Namespace, pod.Name, container, cmdArgs)
}

func (c *Client) SetTargetNode(nodeName string) {
	c.targetNode = nodeName
}

func (c *Client) resolveTargetPod() (kube.OVNPod, error) {
	if c.targetNode != "" {
		return c.findNodePod(c.targetNode)
	}
	if len(c.topology.NodePods) == 0 {
		return kube.OVNPod{}, fmt.Errorf("no ovnkube-node pods available")
	}
	return c.topology.NodePods[0], nil
}

func (c *Client) NodePods() []kube.OVNPod {
	return c.topology.NodePods
}

func (c *Client) ExecInNodePod(ctx context.Context, nodeName, container string, command []string) (string, error) {
	pod, err := c.findNodePod(nodeName)
	if err != nil {
		return "", err
	}
	return c.ExecInPod(ctx, pod.Namespace, pod.Name, container, command)
}

func (c *Client) findNodePod(nodeName string) (kube.OVNPod, error) {
	for _, p := range c.topology.NodePods {
		if p.NodeName == nodeName {
			return p, nil
		}
	}
	return kube.OVNPod{}, fmt.Errorf("no ovnkube-node pod found on node %s", nodeName)
}

func (c *Client) ExecInPod(ctx context.Context, namespace, podName, container string, command []string) (string, error) {
	req := c.kubeClient.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.kubeClient.RestConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("creating executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("%s: %w", errMsg, err)
		}
		return "", err
	}
	return stdout.String(), nil
}

func (c *Client) Topology() *kube.OVNTopology {
	return c.topology
}

func (c *Client) KubeClientset() kubernetes.Interface {
	return c.kubeClient.Clientset
}

func (c *Client) RestConfig() *rest.Config {
	return c.kubeClient.RestConfig
}
