package trace

import (
	"context"
	"fmt"
	"net"

	"github.com/rsevilla/ovnkctl/pkg/kube"
	"github.com/rsevilla/ovnkctl/pkg/ovn"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func NewTraceCmd(kubeFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var srcPod, dstPod, dstIP, port string
	var tcp, udp bool

	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Trace packet flow between pods or to external IPs",
		Long:  "Wrapper around ovnkube-trace that resolves pod names and formats output.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if srcPod == "" {
				return fmt.Errorf("--src is required")
			}
			if dstPod == "" && dstIP == "" {
				return fmt.Errorf("--dst or --dst-ip is required")
			}

			ctx := context.Background()
			kubeClient, err := kube.NewClient(kubeFlags)
			if err != nil {
				return fmt.Errorf("creating kubernetes client: %w", err)
			}
			topo, err := kube.DiscoverOVN(ctx, kubeClient.Clientset)
			if err != nil {
				return fmt.Errorf("discovering OVN-Kubernetes: %w", err)
			}
			ovnClient := ovn.NewClient(kubeClient, topo)

			srcNS, srcName := getNamespace(kubeFlags), srcPod

			traceArgs := []string{
				"ovnkube-trace",
				"-src", srcName,
				"-src-namespace", srcNS,
				"-dst-port", port,
				"-ovn-config-namespace", topo.Namespace,
			}

			if dstPod != "" {
				dstNS, dstName := getNamespace(kubeFlags), dstPod
				traceArgs = append(traceArgs, "-dst", dstName, "-dst-namespace", dstNS)
			} else if dstIP != "" {
				if net.ParseIP(dstIP) == nil {
					return fmt.Errorf("invalid destination IP: %s", dstIP)
				}
				traceArgs = append(traceArgs, "-dst-ip", dstIP)
			}

			if udp {
				traceArgs = append(traceArgs, "-udp")
			} else if tcp {
				traceArgs = append(traceArgs, "-tcp")
			}

			pod := topo.NodePods[0]
			result, err := ovnClient.ExecInNodePod(ctx, pod.NodeName, "ovnkube-controller", traceArgs)
			if err != nil {
				return fmt.Errorf("running ovnkube-trace: %w", err)
			}

			fmt.Print(result)
			return nil
		},
	}

	cmd.Flags().StringVar(&srcPod, "src", "", "Source pod (namespace/name)")
	cmd.Flags().StringVar(&dstPod, "dst", "", "Destination pod (namespace/name)")
	cmd.Flags().StringVar(&dstIP, "dst-ip", "", "Destination IP address")
	cmd.Flags().StringVar(&port, "port", "80", "Destination port")
	cmd.Flags().BoolVar(&tcp, "tcp", true, "Use TCP protocol")
	cmd.Flags().BoolVar(&udp, "udp", false, "Use UDP protocol")

	return cmd
}

func getNamespace(kubeFlags *genericclioptions.ConfigFlags) string {
	if kubeFlags.Namespace != nil && *kubeFlags.Namespace != "" {
		return *kubeFlags.Namespace
	}
	return "default"
}
