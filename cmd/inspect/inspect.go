package inspect

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rsevilla/ovnkctl/pkg/kube"
	"github.com/rsevilla/ovnkctl/pkg/output"
	"github.com/rsevilla/ovnkctl/pkg/ovn"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func NewInspectCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Deep inspection of Kubernetes resources' OVN state",
	}
	cmd.AddCommand(newPodCmd(kubeFlags, outputFormat))
	cmd.AddCommand(newNodeCmd(kubeFlags, outputFormat))
	cmd.AddCommand(newServiceCmd(kubeFlags, outputFormat))
	cmd.AddCommand(newNetworkPolicyCmd(kubeFlags, outputFormat))
	return cmd
}

func initOVNClient(kubeFlags *genericclioptions.ConfigFlags) (*ovn.Client, error) {
	ctx := context.Background()
	kubeClient, err := kube.NewClient(kubeFlags)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}
	topo, err := kube.DiscoverOVN(ctx, kubeClient.Clientset)
	if err != nil {
		return nil, fmt.Errorf("discovering OVN-Kubernetes: %w", err)
	}
	return ovn.NewClient(kubeClient, topo), nil
}

func getNamespace(kubeFlags *genericclioptions.ConfigFlags) string {
	if kubeFlags.Namespace != nil && *kubeFlags.Namespace != "" {
		return *kubeFlags.Namespace
	}
	return "default"
}

func extID(ids map[string]string, keys ...string) string {
	for _, key := range keys {
		if v, ok := ids[key]; ok && v != "" {
			return v
		}
		if v, ok := ids["k8s.ovn.org/"+key]; ok && v != "" {
			return v
		}
	}
	return ""
}

func newPodCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	return &cobra.Command{
		Use:   "pod <name>",
		Short: "Deep inspection of a pod's OVN networking state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			ns, name := getNamespace(kubeFlags), args[0]

			ovnClient, err := initOVNClient(kubeFlags)
			if err != nil {
				return err
			}

			pod, err := ovnClient.KubeClientset().CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("getting pod %s/%s: %w", ns, name, err)
			}

			portName := ns + "_" + name
			ports, err := ovnClient.GetLogicalSwitchPorts(ctx)
			if err != nil {
				return fmt.Errorf("getting logical switch ports: %w", err)
			}

			var lsp *ovn.LogicalSwitchPort
			for _, p := range ports {
				if p.Name == portName {
					lsp = &p
					break
				}
			}

			type PodInspection struct {
				Pod           PodInfo     `json:"pod"`
				LogicalPort   *PortDetail `json:"logical_port,omitempty"`
				ACLs          []ACLDetail `json:"acls,omitempty"`
				LoadBalancers []LBDetail  `json:"load_balancers,omitempty"`
			}
			type result = PodInspection

			podInfo := PodInfo{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				Node:      pod.Spec.NodeName,
				PodIP:     pod.Status.PodIP,
				Phase:     string(pod.Status.Phase),
			}

			r := result{Pod: podInfo}

			if lsp != nil {
				r.LogicalPort = &PortDetail{
					Name:         lsp.Name,
					Addresses:    lsp.Addresses,
					Type:         lsp.Type,
					PortSecurity: lsp.PortSecurity,
				}
			}

			acls, err := ovnClient.GetACLs(ctx)
			if err == nil {
				for _, acl := range acls {
					if strings.Contains(acl.Match, portName) || strings.Contains(acl.Match, pod.Status.PodIP) {
						r.ACLs = append(r.ACLs, ACLDetail{
							Direction: acl.Direction,
							Priority:  acl.Priority,
							Action:    acl.Action,
							Match:     acl.Match,
							Policy:    extID(acl.ExternalIDs, "name"),
						})
					}
				}
			}

			lbs, err := ovnClient.GetAllLoadBalancers(ctx)
			if err == nil {
				for _, lb := range lbs {
					for vip, backends := range lb.VIPs {
						if strings.Contains(backends, pod.Status.PodIP) {
							r.LoadBalancers = append(r.LoadBalancers, LBDetail{
								Name:     lb.Name,
								VIP:      vip,
								Backends: backends,
								Service:  extID(lb.ExternalIDs, "owner"),
							})
						}
					}
				}
			}

			if *outputFormat != "table" {
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(r)
			}

			fmt.Printf("Pod: %s/%s\n", podInfo.Namespace, podInfo.Name)
			fmt.Printf("  Node:   %s\n", podInfo.Node)
			fmt.Printf("  PodIP:  %s\n", podInfo.PodIP)
			fmt.Printf("  Phase:  %s\n", podInfo.Phase)

			if r.LogicalPort != nil {
				fmt.Printf("\nLogical Switch Port: %s\n", r.LogicalPort.Name)
				fmt.Printf("  Addresses:     %s\n", strings.Join(r.LogicalPort.Addresses, ", "))
				if len(r.LogicalPort.PortSecurity) > 0 {
					fmt.Printf("  Port Security: %s\n", strings.Join(r.LogicalPort.PortSecurity, ", "))
				}
			} else {
				fmt.Printf("\nLogical Switch Port: not found\n")
			}

			p := output.NewPrinter("table", os.Stdout)
			if len(r.ACLs) > 0 {
				fmt.Printf("\nMatching ACLs (%d):\n", len(r.ACLs))
				var rows [][]string
				for _, acl := range r.ACLs {
					match := acl.Match
					if len(match) > 60 {
						match = match[:57] + "..."
					}
					rows = append(rows, []string{acl.Policy, acl.Direction, fmt.Sprintf("%d", acl.Priority), acl.Action, match})
				}
				p.PrintTable([]string{"POLICY", "DIRECTION", "PRIORITY", "ACTION", "MATCH"}, rows)
			}

			if len(r.LoadBalancers) > 0 {
				fmt.Printf("\nLoad Balancer VIPs (%d):\n", len(r.LoadBalancers))
				var rows [][]string
				for _, lb := range r.LoadBalancers {
					backends := lb.Backends
					if len(backends) > 50 {
						backends = backends[:47] + "..."
					}
					rows = append(rows, []string{lb.Service, lb.VIP, backends})
				}
				p.PrintTable([]string{"SERVICE", "VIP", "BACKENDS"}, rows)
			}

			return nil
		},
	}
}

type PodInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Node      string `json:"node"`
	PodIP     string `json:"pod_ip"`
	Phase     string `json:"phase"`
}

type PortDetail struct {
	Name         string   `json:"name"`
	Addresses    []string `json:"addresses"`
	Type         string   `json:"type"`
	PortSecurity []string `json:"port_security"`
}

type ACLDetail struct {
	Direction string `json:"direction"`
	Priority  int    `json:"priority"`
	Action    string `json:"action"`
	Match     string `json:"match"`
	Policy    string `json:"policy"`
}

type LBDetail struct {
	Name     string `json:"name"`
	VIP      string `json:"vip"`
	Backends string `json:"backends"`
	Service  string `json:"service"`
}

func newNodeCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	return &cobra.Command{
		Use:   "node <name>",
		Short: "Deep inspection of a node's OVN/OVS state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			nodeName := args[0]

			ovnClient, err := initOVNClient(kubeFlags)
			if err != nil {
				return err
			}

			node, err := ovnClient.KubeClientset().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("getting node %s: %w", nodeName, err)
			}

			type NodeInspection struct {
				Name       string         `json:"name"`
				InternalIP string         `json:"internal_ip"`
				Chassis    *ChassisDetail `json:"chassis,omitempty"`
				Bridges    string         `json:"bridges,omitempty"`
				PodCount   int            `json:"pod_count"`
			}

			internalIP := ""
			for _, addr := range node.Status.Addresses {
				if addr.Type == "InternalIP" {
					internalIP = addr.Address
					break
				}
			}

			r := NodeInspection{
				Name:       nodeName,
				InternalIP: internalIP,
			}

			sbShow, err := ovnClient.SBCtlShow(ctx)
			if err == nil {
				chassisList := ovn.ParseSBCtlShow(sbShow)
				for _, ch := range chassisList {
					if ch.Hostname == nodeName {
						cd := &ChassisDetail{
							Name:     ch.Name,
							Hostname: ch.Hostname,
						}
						for _, enc := range ch.Encaps {
							cd.Encaps = append(cd.Encaps, fmt.Sprintf("%s:%s", enc.Type, enc.IP))
						}
						r.Chassis = cd
						break
					}
				}
			}

			ovsShow, err := ovnClient.OVSCtl(ctx, nodeName, "show")
			if err == nil {
				r.Bridges = ovsShow
			}

			pods, err := ovnClient.KubeClientset().CoreV1().Pods("").List(ctx, metav1.ListOptions{
				FieldSelector: "spec.nodeName=" + nodeName,
			})
			if err == nil {
				r.PodCount = len(pods.Items)
			}

			if *outputFormat != "table" {
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(r)
			}

			fmt.Printf("Node: %s\n", r.Name)
			fmt.Printf("  Internal IP: %s\n", r.InternalIP)
			fmt.Printf("  Pod Count:   %d\n", r.PodCount)

			if r.Chassis != nil {
				fmt.Printf("\nChassis: %s\n", r.Chassis.Name)
				fmt.Printf("  Hostname: %s\n", r.Chassis.Hostname)
				for _, enc := range r.Chassis.Encaps {
					fmt.Printf("  Encap:    %s\n", enc)
				}
			}

			if r.Bridges != "" {
				fmt.Printf("\nOVS Bridges:\n%s\n", r.Bridges)
			}

			return nil
		},
	}
}

type ChassisDetail struct {
	Name     string   `json:"name"`
	Hostname string   `json:"hostname"`
	Encaps   []string `json:"encaps"`
}

func newServiceCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	return &cobra.Command{
		Use:   "service <name>",
		Short: "Inspect how a Kubernetes service is realized in OVN",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			ns, name := getNamespace(kubeFlags), args[0]

			ovnClient, err := initOVNClient(kubeFlags)
			if err != nil {
				return err
			}

			svc, err := ovnClient.KubeClientset().CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("getting service %s/%s: %w", ns, name, err)
			}

			type ServiceInspection struct {
				Name      string    `json:"name"`
				Namespace string    `json:"namespace"`
				ClusterIP string    `json:"cluster_ip"`
				Type      string    `json:"type"`
				LBs       []LBMatch `json:"load_balancers"`
			}

			r := ServiceInspection{
				Name:      svc.Name,
				Namespace: svc.Namespace,
				ClusterIP: svc.Spec.ClusterIP,
				Type:      string(svc.Spec.Type),
			}

			svcKey := ns + "/" + name
			lbs, err := ovnClient.GetAllLoadBalancers(ctx)
			if err == nil {
				for _, lb := range lbs {
					owner := extID(lb.ExternalIDs, "owner")
					if owner == svcKey {
						for vip, backends := range lb.VIPs {
							r.LBs = append(r.LBs, LBMatch{
								Name:     lb.Name,
								Protocol: lb.Protocol,
								VIP:      vip,
								Backends: backends,
							})
						}
					}
				}
			}

			if *outputFormat != "table" {
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(r)
			}

			fmt.Printf("Service: %s/%s\n", r.Namespace, r.Name)
			fmt.Printf("  ClusterIP: %s\n", r.ClusterIP)
			fmt.Printf("  Type:      %s\n", r.Type)

			if len(r.LBs) > 0 {
				fmt.Printf("\nOVN Load Balancer VIPs (%d):\n", len(r.LBs))
				p := output.NewPrinter("table", os.Stdout)
				var rows [][]string
				for _, lb := range r.LBs {
					backends := lb.Backends
					if len(backends) > 60 {
						backends = backends[:57] + "..."
					}
					rows = append(rows, []string{lb.Name, lb.Protocol, lb.VIP, backends})
				}
				p.PrintTable([]string{"LB NAME", "PROTOCOL", "VIP", "BACKENDS"}, rows)
			} else {
				fmt.Printf("\nNo OVN load balancers found for this service.\n")
			}

			return nil
		},
	}
}

type LBMatch struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	VIP      string `json:"vip"`
	Backends string `json:"backends"`
}

func newNetworkPolicyCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	return &cobra.Command{
		Use:   "networkpolicy <name>",
		Short: "Inspect how a NetworkPolicy is realized in OVN",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			ns, name := getNamespace(kubeFlags), args[0]

			ovnClient, err := initOVNClient(kubeFlags)
			if err != nil {
				return err
			}

			_, err = ovnClient.KubeClientset().NetworkingV1().NetworkPolicies(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("getting networkpolicy %s/%s: %w", ns, name, err)
			}

			type PolicyInspection struct {
				Name      string      `json:"name"`
				Namespace string      `json:"namespace"`
				ACLs      []ACLDetail `json:"acls"`
			}

			r := PolicyInspection{
				Name:      name,
				Namespace: ns,
			}

			policyKey := ns + ":" + name
			acls, err := ovnClient.GetACLs(ctx)
			if err == nil {
				for _, acl := range acls {
					aclName := extID(acl.ExternalIDs, "name")
					aclID := extID(acl.ExternalIDs, "id")
					ownerType := extID(acl.ExternalIDs, "owner-type")

					match := false
					if ownerType == "NetworkPolicy" && aclName == policyKey {
						match = true
					} else if ownerType == "NetpolNamespace" && aclName == ns {
						match = true
					} else if strings.Contains(aclID, ":NetworkPolicy:"+policyKey+":") {
						match = true
					}

					if match {
						r.ACLs = append(r.ACLs, ACLDetail{
							Direction: acl.Direction,
							Priority:  acl.Priority,
							Action:    acl.Action,
							Match:     acl.Match,
							Policy:    aclName,
						})
					}
				}
			}

			if *outputFormat != "table" {
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(r)
			}

			fmt.Printf("NetworkPolicy: %s/%s\n\n", r.Namespace, r.Name)

			if len(r.ACLs) > 0 {
				fmt.Printf("Generated ACLs (%d):\n", len(r.ACLs))
				p := output.NewPrinter("table", os.Stdout)
				var rows [][]string
				for _, acl := range r.ACLs {
					match := acl.Match
					if len(match) > 60 {
						match = match[:57] + "..."
					}
					rows = append(rows, []string{acl.Direction, fmt.Sprintf("%d", acl.Priority), acl.Action, match})
				}
				p.PrintTable([]string{"DIRECTION", "PRIORITY", "ACTION", "MATCH"}, rows)
			} else {
				fmt.Printf("No ACLs found for this network policy.\n")
			}

			return nil
		},
	}
}
