package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rsevilla/ovnkctl/pkg/kube"
	"github.com/rsevilla/ovnkctl/pkg/output"
	"github.com/rsevilla/ovnkctl/pkg/ovn"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type StatusResult struct {
	OVNNamespace    string            `json:"ovn_namespace" yaml:"ovn_namespace"`
	NorthdVersion   string            `json:"northd_version" yaml:"northd_version"`
	NBDBStatus      string            `json:"nbdb_status" yaml:"nbdb_status"`
	SBDBStatus      string            `json:"sbdb_status" yaml:"sbdb_status"`
	NorthdStatus    string            `json:"northd_status" yaml:"northd_status"`
	ConfigSync      ConfigSyncStatus  `json:"config_sync" yaml:"config_sync"`
	NodePods        []PodStatus       `json:"node_pods" yaml:"node_pods"`
	ControlPlane    []PodStatus       `json:"control_plane_pods" yaml:"control_plane_pods"`
	Chassis         []ChassisStatus   `json:"chassis" yaml:"chassis"`
	CRDCounts       map[string]int    `json:"crd_counts" yaml:"crd_counts"`
}

type ConfigSyncStatus struct {
	NBCfg int `json:"nb_cfg" yaml:"nb_cfg"`
	SBCfg int `json:"sb_cfg" yaml:"sb_cfg"`
	HVCfg int `json:"hv_cfg" yaml:"hv_cfg"`
	InSync bool `json:"in_sync" yaml:"in_sync"`
}

type PodStatus struct {
	Name   string `json:"name" yaml:"name"`
	Node   string `json:"node" yaml:"node"`
	Ready  bool   `json:"ready" yaml:"ready"`
}

type ChassisStatus struct {
	Name     string `json:"name" yaml:"name"`
	Hostname string `json:"hostname" yaml:"hostname"`
	TunnelIP string `json:"tunnel_ip" yaml:"tunnel_ip"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show OVN-Kubernetes health status",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	kubeClient, err := kube.NewClient(kubeConfigFlags)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	topo, err := kube.DiscoverOVN(ctx, kubeClient.Clientset)
	if err != nil {
		return fmt.Errorf("discovering OVN-Kubernetes: %w", err)
	}

	ovnClient := ovn.NewClient(kubeClient, topo)
	result := &StatusResult{
		OVNNamespace: topo.Namespace,
		CRDCounts:    make(map[string]int),
	}

	for _, p := range topo.NodePods {
		result.NodePods = append(result.NodePods, PodStatus{Name: p.Name, Node: p.NodeName, Ready: p.Ready})
	}
	for _, p := range topo.ControlPlanePods {
		result.ControlPlane = append(result.ControlPlane, PodStatus{Name: p.Name, Node: p.NodeName, Ready: p.Ready})
	}

	nbGlobal, err := ovnClient.GetNBGlobal(ctx)
	if err == nil {
		result.NorthdVersion = nbGlobal.NorthdVersion
		result.ConfigSync = ConfigSyncStatus{
			NBCfg:  nbGlobal.NBCfg,
			SBCfg:  nbGlobal.SBCfg,
			HVCfg:  nbGlobal.HVCfg,
			InSync: nbGlobal.NBCfg == nbGlobal.SBCfg && nbGlobal.SBCfg == nbGlobal.HVCfg,
		}
	}

	nbStatus, err := ovnClient.GetNBDBStatus(ctx)
	if err == nil {
		result.NBDBStatus = strings.TrimSpace(nbStatus)
	} else {
		result.NBDBStatus = "error"
	}

	sbStatus, err := ovnClient.GetSBDBStatus(ctx)
	if err == nil {
		result.SBDBStatus = strings.TrimSpace(sbStatus)
	} else {
		result.SBDBStatus = "error"
	}

	northdStatus, _ := ovnClient.GetNorthdStatus(ctx)
	result.NorthdStatus = northdStatus

	sbShow, err := ovnClient.SBCtlShow(ctx)
	if err == nil {
		chassisList := ovn.ParseSBCtlShow(sbShow)
		for _, ch := range chassisList {
			cs := ChassisStatus{Name: ch.Name, Hostname: ch.Hostname}
			if len(ch.Encaps) > 0 {
				cs.TunnelIP = ch.Encaps[0].IP
			}
			result.Chassis = append(result.Chassis, cs)
		}
	}

	dynClient, err := dynamic.NewForConfig(kubeClient.RestConfig)
	if err == nil {
		ovnCRDs := map[string]schema.GroupVersionResource{
			"EgressIP":                     {Group: "k8s.ovn.org", Version: "v1", Resource: "egressips"},
			"EgressFirewall":               {Group: "k8s.ovn.org", Version: "v1", Resource: "egressfirewalls"},
			"EgressQoS":                    {Group: "k8s.ovn.org", Version: "v1", Resource: "egressqoses"},
			"EgressService":                {Group: "k8s.ovn.org", Version: "v1", Resource: "egressservices"},
			"UserDefinedNetwork":           {Group: "k8s.ovn.org", Version: "v1", Resource: "userdefinednetworks"},
			"ClusterUserDefinedNetwork":    {Group: "k8s.ovn.org", Version: "v1", Resource: "clusteruserdefinednetworks"},
			"AdminPolicyBasedExternalRoute": {Group: "k8s.ovn.org", Version: "v1", Resource: "adminpolicybasedexternalroutes"},
		}
		for name, gvr := range ovnCRDs {
			list, err := dynClient.Resource(gvr).List(ctx, metav1.ListOptions{})
			if err == nil {
				result.CRDCounts[name] = len(list.Items)
			}
		}
	}

	return printStatus(result)
}

func printStatus(result *StatusResult) error {
	switch outputFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case "yaml":
		p := output.NewPrinter("yaml", os.Stdout)
		return p.Print(result)
	default:
		return printStatusTable(result)
	}
}

func printStatusTable(r *StatusResult) error {
	fmt.Printf("OVN-Kubernetes Status\n")
	fmt.Printf("=====================\n\n")

	fmt.Printf("Namespace:       %s\n", r.OVNNamespace)
	fmt.Printf("Northd Version:  %s\n", r.NorthdVersion)
	fmt.Printf("Northd:          %s\n", r.NorthdStatus)
	fmt.Printf("NB DB:           %s\n", statusIcon(r.NBDBStatus))
	fmt.Printf("SB DB:           %s\n", statusIcon(r.SBDBStatus))

	syncStatus := "in sync"
	if !r.ConfigSync.InSync {
		syncStatus = fmt.Sprintf("out of sync (nb=%d sb=%d hv=%d)", r.ConfigSync.NBCfg, r.ConfigSync.SBCfg, r.ConfigSync.HVCfg)
	}
	fmt.Printf("Config Sync:     %s (gen %d)\n", syncStatus, r.ConfigSync.NBCfg)

	fmt.Printf("\nControl Plane Pods (%d):\n", len(r.ControlPlane))
	p := output.NewPrinter("table", os.Stdout)
	var cpRows [][]string
	for _, pod := range r.ControlPlane {
		cpRows = append(cpRows, []string{pod.Name, pod.Node, readyStr(pod.Ready)})
	}
	p.PrintTable([]string{"NAME", "NODE", "READY"}, cpRows)

	fmt.Printf("\nNode Pods (%d):\n", len(r.NodePods))
	var nodeRows [][]string
	for _, pod := range r.NodePods {
		nodeRows = append(nodeRows, []string{pod.Name, pod.Node, readyStr(pod.Ready)})
	}
	p.PrintTable([]string{"NAME", "NODE", "READY"}, nodeRows)

	fmt.Printf("\nChassis (%d):\n", len(r.Chassis))
	var chassisRows [][]string
	for _, ch := range r.Chassis {
		chassisRows = append(chassisRows, []string{ch.Hostname, ch.TunnelIP})
	}
	p.PrintTable([]string{"HOSTNAME", "TUNNEL IP"}, chassisRows)

	if len(r.CRDCounts) > 0 {
		fmt.Printf("\nOVN CRDs:\n")
		var crdRows [][]string
		for name, count := range r.CRDCounts {
			crdRows = append(crdRows, []string{name, fmt.Sprintf("%d", count)})
		}
		p.PrintTable([]string{"RESOURCE", "COUNT"}, crdRows)
	}

	return nil
}

func statusIcon(status string) string {
	if strings.Contains(status, "ok") {
		return status + " (healthy)"
	}
	return status + " (degraded)"
}

func readyStr(ready bool) string {
	if ready {
		return "Ready"
	}
	return "NotReady"
}
