package show

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/rsevilla/ovnkctl/pkg/kube"
	"github.com/rsevilla/ovnkctl/pkg/output"
	"github.com/rsevilla/ovnkctl/pkg/ovn"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func NewShowCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show OVN-Kubernetes resources and state",
	}
	cmd.AddCommand(newTopologyCmd(kubeFlags, outputFormat))
	cmd.AddCommand(newPodsCmd(kubeFlags, outputFormat))
	cmd.AddCommand(newACLsCmd(kubeFlags, outputFormat))
	cmd.AddCommand(newLoadBalancersCmd(kubeFlags, outputFormat))
	cmd.AddCommand(newPortsCmd(kubeFlags, outputFormat))
	return cmd
}

func initOVNClient(kubeFlags *genericclioptions.ConfigFlags, targetNode string) (*ovn.Client, error) {
	ctx := context.Background()
	kubeClient, err := kube.NewClient(kubeFlags)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}
	topo, err := kube.DiscoverOVN(ctx, kubeClient.Clientset)
	if err != nil {
		return nil, fmt.Errorf("discovering OVN-Kubernetes: %w", err)
	}
	client := ovn.NewClient(kubeClient, topo)
	if targetNode != "" {
		client.SetTargetNode(targetNode)
	}
	return client, nil
}

func getNamespace(kubeFlags *genericclioptions.ConfigFlags) string {
	if kubeFlags.Namespace != nil && *kubeFlags.Namespace != "" {
		return *kubeFlags.Namespace
	}
	return ""
}

func getSwitches(ctx context.Context, ovnClient *ovn.Client, node string) ([]ovn.LogicalSwitch, error) {
	if node != "" {
		return ovnClient.GetLogicalSwitches(ctx)
	}
	return ovnClient.GetAllLogicalSwitches(ctx)
}

func getRouters(ctx context.Context, ovnClient *ovn.Client, node string) ([]ovn.LogicalRouter, error) {
	if node != "" {
		return ovnClient.GetLogicalRouters(ctx)
	}
	return ovnClient.GetAllLogicalRouters(ctx)
}

func getACLs(ctx context.Context, ovnClient *ovn.Client, node string) ([]ovn.ACL, error) {
	if node != "" {
		return ovnClient.GetACLs(ctx)
	}
	return ovnClient.GetAllACLs(ctx)
}

func getLBs(ctx context.Context, ovnClient *ovn.Client, node string) ([]ovn.LoadBalancer, error) {
	if node != "" {
		return ovnClient.GetLoadBalancers(ctx)
	}
	return ovnClient.GetAllLoadBalancers(ctx)
}

func getLSPs(ctx context.Context, ovnClient *ovn.Client, node string) ([]ovn.LogicalSwitchPort, error) {
	if node != "" {
		return ovnClient.GetLogicalSwitchPorts(ctx)
	}
	return ovnClient.GetAllLogicalSwitchPorts(ctx)
}

func newTopologyCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	var node string
	cmd := &cobra.Command{
		Use:   "topology",
		Short: "Show OVN logical network topology",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			ovnClient, err := initOVNClient(kubeFlags, node)
			if err != nil {
				return err
			}

			switches, err := getSwitches(ctx, ovnClient, node)
			if err != nil {
				return fmt.Errorf("getting logical switches: %w", err)
			}
			routers, err := getRouters(ctx, ovnClient, node)
			if err != nil {
				return fmt.Errorf("getting logical routers: %w", err)
			}
			chassisList := getChassis(ctx, ovnClient)

			if *outputFormat != "table" {
				data := map[string]any{
					"logical_switches": switches,
					"logical_routers":  routers,
					"chassis":          chassisList,
				}
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(data)
			}

			p := output.NewPrinter("table", os.Stdout)

			fmt.Printf("Logical Routers (%d):\n", len(routers))
			var rRows [][]string
			for _, r := range routers {
				rRows = append(rRows, []string{r.Name, fmt.Sprintf("%d", len(r.Ports)), shortNode(r.SourceNode)})
			}
			p.PrintTable([]string{"NAME", "PORTS", "NODE"}, rRows)

			fmt.Printf("\nLogical Switches (%d):\n", len(switches))
			var sRows [][]string
			for _, s := range switches {
				sRows = append(sRows, []string{s.Name, fmt.Sprintf("%d", len(s.Ports)), shortNode(s.SourceNode)})
			}
			sort.Slice(sRows, func(i, j int) bool { return sRows[i][0] < sRows[j][0] })
			p.PrintTable([]string{"NAME", "PORTS", "NODE"}, sRows)

			if len(chassisList) > 0 {
				fmt.Printf("\nChassis (%d):\n", len(chassisList))
				var cRows [][]string
				for _, ch := range chassisList {
					tunnelIP := ""
					encapType := ""
					if len(ch.Encaps) > 0 {
						tunnelIP = ch.Encaps[0].IP
						encapType = ch.Encaps[0].Type
					}
					cRows = append(cRows, []string{ch.Hostname, tunnelIP, encapType})
				}
				p.PrintTable([]string{"HOSTNAME", "TUNNEL IP", "ENCAP"}, cRows)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "Query a specific node (default: all nodes)")
	return cmd
}

func newPodsCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	var node string
	cmd := &cobra.Command{
		Use:   "pods",
		Short: "Show pods with OVN networking details",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			namespace := getNamespace(kubeFlags)
			ovnClient, err := initOVNClient(kubeFlags, node)
			if err != nil {
				return err
			}

			ports, err := getLSPs(ctx, ovnClient, node)
			if err != nil {
				return fmt.Errorf("getting logical switch ports: %w", err)
			}

			type PodOVNInfo struct {
				Namespace    string   `json:"namespace"`
				Pod          string   `json:"pod"`
				Node         string   `json:"node"`
				LogicalPort  string   `json:"logical_port"`
				Addresses    []string `json:"addresses"`
				PortSecurity []string `json:"port_security"`
			}

			var podInfos []PodOVNInfo
			for _, port := range ports {
				ns, pod := extractPodFromPort(port)
				if pod == "" {
					continue
				}
				if namespace != "" && ns != namespace {
					continue
				}
				podInfos = append(podInfos, PodOVNInfo{
					Namespace:    ns,
					Pod:          pod,
					Node:         port.SourceNode,
					LogicalPort:  port.Name,
					Addresses:    port.Addresses,
					PortSecurity: port.PortSecurity,
				})
			}

			sort.Slice(podInfos, func(i, j int) bool {
				if podInfos[i].Namespace != podInfos[j].Namespace {
					return podInfos[i].Namespace < podInfos[j].Namespace
				}
				return podInfos[i].Pod < podInfos[j].Pod
			})

			if *outputFormat != "table" {
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(podInfos)
			}

			p := output.NewPrinter("table", os.Stdout)
			fmt.Printf("Pods with OVN networking (%d):\n", len(podInfos))
			var rows [][]string
			for _, info := range podInfos {
				addr := strings.Join(info.Addresses, ", ")
				rows = append(rows, []string{info.Namespace, info.Pod, shortNode(info.Node), addr})
			}
			p.PrintTable([]string{"NAMESPACE", "POD", "NODE", "ADDRESSES"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "Query a specific node (default: all nodes)")
	return cmd
}

func newACLsCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	var policy, node string
	cmd := &cobra.Command{
		Use:   "acls",
		Short: "Show OVN ACLs with Kubernetes context",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			namespace := getNamespace(kubeFlags)
			ovnClient, err := initOVNClient(kubeFlags, node)
			if err != nil {
				return err
			}

			acls, err := getACLs(ctx, ovnClient, node)
			if err != nil {
				return fmt.Errorf("getting ACLs: %w", err)
			}

			type ACLInfo struct {
				PolicyNamespace string `json:"policy_namespace"`
				PolicyName      string `json:"policy_name"`
				Node            string `json:"node"`
				Direction       string `json:"direction"`
				Priority        int    `json:"priority"`
				Action          string `json:"action"`
				Match           string `json:"match"`
			}

			var aclInfos []ACLInfo
			for _, acl := range acls {
				policyNS := extID(acl.ExternalIDs, "owner-controller")
				policyName := extID(acl.ExternalIDs, "name")

				if namespace != "" && policyNS != namespace {
					continue
				}
				if policy != "" && policyName != policy {
					continue
				}

				aclInfos = append(aclInfos, ACLInfo{
					PolicyNamespace: policyNS,
					PolicyName:      policyName,
					Node:            acl.SourceNode,
					Direction:       acl.Direction,
					Priority:        acl.Priority,
					Action:          acl.Action,
					Match:           acl.Match,
				})
			}

			sort.Slice(aclInfos, func(i, j int) bool {
				if aclInfos[i].PolicyNamespace != aclInfos[j].PolicyNamespace {
					return aclInfos[i].PolicyNamespace < aclInfos[j].PolicyNamespace
				}
				if aclInfos[i].PolicyName != aclInfos[j].PolicyName {
					return aclInfos[i].PolicyName < aclInfos[j].PolicyName
				}
				return aclInfos[i].Priority > aclInfos[j].Priority
			})

			if *outputFormat != "table" {
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(aclInfos)
			}

			p := output.NewPrinter("table", os.Stdout)
			fmt.Printf("ACLs (%d):\n", len(aclInfos))
			var rows [][]string
			for _, info := range aclInfos {
				match := info.Match
				if len(match) > 60 {
					match = match[:57] + "..."
				}
				rows = append(rows, []string{
					info.PolicyNamespace,
					info.PolicyName,
					shortNode(info.Node),
					info.Direction,
					fmt.Sprintf("%d", info.Priority),
					info.Action,
					match,
				})
			}
			p.PrintTable([]string{"OWNER", "POLICY", "NODE", "DIRECTION", "PRIORITY", "ACTION", "MATCH"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&policy, "policy", "", "Filter by policy name")
	cmd.Flags().StringVar(&node, "node", "", "Query a specific node (default: all nodes)")
	return cmd
}

func newLoadBalancersCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	var service, node string
	cmd := &cobra.Command{
		Use:     "load-balancers",
		Aliases: []string{"lbs"},
		Short:   "Show OVN load balancers mapped to Kubernetes services",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			ovnClient, err := initOVNClient(kubeFlags, node)
			if err != nil {
				return err
			}

			lbs, err := getLBs(ctx, ovnClient, node)
			if err != nil {
				return fmt.Errorf("getting load balancers: %w", err)
			}

			type LBInfo struct {
				Name     string            `json:"name"`
				Node     string            `json:"node"`
				Protocol string            `json:"protocol"`
				VIPs     map[string]string `json:"vips"`
				Service  string            `json:"service"`
			}

			var lbInfos []LBInfo
			for _, lb := range lbs {
				svcName := extID(lb.ExternalIDs, "owner")
				if service != "" && svcName != service {
					continue
				}
				lbInfos = append(lbInfos, LBInfo{
					Name:     lb.Name,
					Node:     lb.SourceNode,
					Protocol: lb.Protocol,
					VIPs:     lb.VIPs,
					Service:  svcName,
				})
			}

			if *outputFormat != "table" {
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(lbInfos)
			}

			p := output.NewPrinter("table", os.Stdout)
			fmt.Printf("Load Balancers (%d):\n", len(lbInfos))
			var rows [][]string
			for _, lb := range lbInfos {
				vipCount := fmt.Sprintf("%d", len(lb.VIPs))
				rows = append(rows, []string{lb.Name, lb.Service, shortNode(lb.Node), lb.Protocol, vipCount})
			}
			p.PrintTable([]string{"NAME", "SERVICE", "NODE", "PROTOCOL", "VIPS"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "Filter by Kubernetes service name")
	cmd.Flags().StringVar(&node, "node", "", "Query a specific node (default: all nodes)")
	return cmd
}

func newPortsCmd(kubeFlags *genericclioptions.ConfigFlags, outputFormat *string) *cobra.Command {
	var node string
	cmd := &cobra.Command{
		Use:   "ports",
		Short: "Show OVN logical switch ports",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			ovnClient, err := initOVNClient(kubeFlags, node)
			if err != nil {
				return err
			}

			ports, err := getLSPs(ctx, ovnClient, node)
			if err != nil {
				return fmt.Errorf("getting logical switch ports: %w", err)
			}

			type PortInfo struct {
				Name         string   `json:"name"`
				Type         string   `json:"type"`
				Namespace    string   `json:"namespace"`
				Pod          string   `json:"pod"`
				Node         string   `json:"node"`
				Addresses    []string `json:"addresses"`
				PortSecurity []string `json:"port_security"`
			}

			var portInfos []PortInfo
			for _, port := range ports {
				ns, podName := extractPodFromPort(port)

				portInfos = append(portInfos, PortInfo{
					Name:         port.Name,
					Type:         port.Type,
					Namespace:    ns,
					Pod:          podName,
					Node:         port.SourceNode,
					Addresses:    port.Addresses,
					PortSecurity: port.PortSecurity,
				})
			}

			sort.Slice(portInfos, func(i, j int) bool { return portInfos[i].Name < portInfos[j].Name })

			if *outputFormat != "table" {
				p := output.NewPrinter(*outputFormat, os.Stdout)
				return p.Print(portInfos)
			}

			p := output.NewPrinter("table", os.Stdout)
			fmt.Printf("Logical Switch Ports (%d):\n", len(portInfos))
			var rows [][]string
			for _, info := range portInfos {
				addr := strings.Join(info.Addresses, ", ")
				if len(addr) > 40 {
					addr = addr[:37] + "..."
				}
				typStr := info.Type
				if typStr == "" {
					typStr = "VIF"
				}
				rows = append(rows, []string{info.Name, typStr, info.Namespace, info.Pod, shortNode(info.Node), addr})
			}
			p.PrintTable([]string{"PORT", "TYPE", "NAMESPACE", "POD", "NODE", "ADDRESSES"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "Query a specific node (default: all nodes)")
	return cmd
}

func shortNode(fqdn string) string {
	if i := strings.IndexByte(fqdn, '.'); i > 0 {
		return fqdn[:i]
	}
	return fqdn
}

func extractPodFromPort(port ovn.LogicalSwitchPort) (namespace, pod string) {
	ns := extID(port.ExternalIDs, "namespace")
	if ns == "" {
		return "", ""
	}
	isPod := extID(port.ExternalIDs, "pod")
	if isPod != "true" {
		return "", ""
	}
	prefix := ns + "_"
	if strings.HasPrefix(port.Name, prefix) {
		return ns, strings.TrimPrefix(port.Name, prefix)
	}
	return ns, port.Name
}

func extID(ids map[string]string, keys ...string) string {
	for _, key := range keys {
		if v, ok := ids[key]; ok {
			return v
		}
		if v, ok := ids["k8s.ovn.org/"+key]; ok {
			return v
		}
	}
	return ""
}

func getChassis(ctx context.Context, ovnClient *ovn.Client) []ovn.Chassis {
	sbShow, err := ovnClient.SBCtlShow(ctx)
	if err != nil {
		return nil
	}
	return ovn.ParseSBCtlShow(sbShow)
}
