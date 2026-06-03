package trace

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/rsevilla/ovnkctl/pkg/kube"
	"github.com/rsevilla/ovnkctl/pkg/ovn"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func NewTraceCmd(kubeFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var srcPod, dstPod, dstIP, port string
	var tcp, udp bool

	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Trace packet flow between pods or to external IPs",
		Long:  "Uses ovn-trace to simulate packet flow through the OVN logical network.",
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

			srcNS := getNamespace(kubeFlags)
			srcPodObj, err := kubeClient.Clientset.CoreV1().Pods(srcNS).Get(ctx, srcPod, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("getting source pod %s/%s: %w", srcNS, srcPod, err)
			}
			srcAnnotation := srcPodObj.Annotations["k8s.ovn.org/pod-networks"]
			if srcAnnotation == "" {
				return fmt.Errorf("source pod %s/%s has no OVN network annotation", srcNS, srcPod)
			}
			srcNet, err := ovn.ParsePodNetworkAnnotation(srcAnnotation)
			if err != nil {
				return err
			}

			if dstPod != "" {
				dstNS := getNamespace(kubeFlags)
				dstPodObj, err := kubeClient.Clientset.CoreV1().Pods(dstNS).Get(ctx, dstPod, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("getting destination pod %s/%s: %w", dstNS, dstPod, err)
				}
				dstAnnotation := dstPodObj.Annotations["k8s.ovn.org/pod-networks"]
				if dstAnnotation == "" {
					return fmt.Errorf("destination pod %s/%s has no OVN network annotation", dstNS, dstPod)
				}
				dstNet, err := ovn.ParsePodNetworkAnnotation(dstAnnotation)
				if err != nil {
					return err
				}
				dstIP = dstNet.IPAddress
			} else if net.ParseIP(dstIP) == nil {
				return fmt.Errorf("invalid destination IP: %s", dstIP)
			}

			gwMAC, err := ovn.IPToMAC(srcNet.GatewayIP)
			if err != nil {
				return fmt.Errorf("deriving gateway MAC: %w", err)
			}

			logicalPort := fmt.Sprintf("%s_%s", srcNS, srcPod)
			datapath := srcPodObj.Spec.NodeName
			proto := "tcp"
			if udp {
				proto = "udp"
			}
			microflow := fmt.Sprintf(
				`inport=="%s" && eth.src==%s && eth.dst==%s && ip4.src==%s && ip4.dst==%s && ip.ttl==64 && %s.dst==%s`,
				logicalPort, srcNet.MACAddress, gwMAC, srcNet.IPAddress, dstIP, proto, port,
			)

			traceCmd := []string{"ovn-trace", datapath, microflow}
			result, err := ovnClient.ExecInNodePod(ctx, srcPodObj.Spec.NodeName, topo.NBDBContainer, traceCmd)
			if err != nil {
				return fmt.Errorf("running ovn-trace: %w", err)
			}

			fmt.Print(result)
			fmt.Printf("\nExecuted command:\n%s\n", strings.Join(traceCmd, " "))
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
