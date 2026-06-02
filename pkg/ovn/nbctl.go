package ovn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type NBGlobal struct {
	UUID            string
	Name            string
	NorthdVersion   string
	NBCfg           int
	SBCfg           int
	HVCfg           int
	Options         map[string]string
}

type LogicalSwitch struct {
	UUID       string
	Name       string
	Ports      []string
	SourceNode string `json:"source_node,omitempty" yaml:"source_node,omitempty"`
}

type LogicalRouter struct {
	UUID       string
	Name       string
	Ports      []string
	SourceNode string `json:"source_node,omitempty" yaml:"source_node,omitempty"`
}

type ACL struct {
	UUID        string
	Action      string
	Direction   string
	Match       string
	Priority    int
	Name        string
	ExternalIDs map[string]string
	SourceNode  string `json:"source_node,omitempty" yaml:"source_node,omitempty"`
}

type LoadBalancer struct {
	UUID        string
	Name        string
	Protocol    string
	VIPs        map[string]string
	ExternalIDs map[string]string
	SourceNode  string `json:"source_node,omitempty" yaml:"source_node,omitempty"`
}

type LogicalSwitchPort struct {
	UUID         string
	Name         string
	Addresses    []string
	Type         string
	ExternalIDs  map[string]string
	PortSecurity []string
	SourceNode   string `json:"source_node,omitempty" yaml:"source_node,omitempty"`
}

func (c *Client) GetNBGlobal(ctx context.Context) (*NBGlobal, error) {
	out, err := c.NBCtl(ctx, "--format=json", "list", "NB_Global")
	if err != nil {
		return nil, err
	}
	return parseNBGlobal(out)
}

func (c *Client) GetLogicalSwitches(ctx context.Context) ([]LogicalSwitch, error) {
	out, err := c.NBCtl(ctx, "--format=json", "list", "Logical_Switch")
	if err != nil {
		return nil, err
	}
	return parseLogicalSwitches(out)
}

func (c *Client) GetLogicalRouters(ctx context.Context) ([]LogicalRouter, error) {
	out, err := c.NBCtl(ctx, "--format=json", "list", "Logical_Router")
	if err != nil {
		return nil, err
	}
	return parseLogicalRouters(out)
}

func (c *Client) GetACLs(ctx context.Context) ([]ACL, error) {
	out, err := c.NBCtl(ctx, "--format=json", "list", "ACL")
	if err != nil {
		return nil, err
	}
	return parseACLs(out)
}

func (c *Client) GetLoadBalancers(ctx context.Context) ([]LoadBalancer, error) {
	out, err := c.NBCtl(ctx, "--format=json", "list", "Load_Balancer")
	if err != nil {
		return nil, err
	}
	return parseLoadBalancers(out)
}

func (c *Client) GetLogicalSwitchPorts(ctx context.Context) ([]LogicalSwitchPort, error) {
	out, err := c.NBCtl(ctx, "--format=json", "list", "Logical_Switch_Port")
	if err != nil {
		return nil, err
	}
	return parseLogicalSwitchPorts(out)
}

func (c *Client) GetAllLogicalSwitches(ctx context.Context) ([]LogicalSwitch, error) {
	return collectFromAllNodes[LogicalSwitch, *LogicalSwitch](ctx, c, "Logical_Switch", parseLogicalSwitches)
}

func (c *Client) GetAllLogicalRouters(ctx context.Context) ([]LogicalRouter, error) {
	return collectFromAllNodes[LogicalRouter, *LogicalRouter](ctx, c, "Logical_Router", parseLogicalRouters)
}

func (c *Client) GetAllACLs(ctx context.Context) ([]ACL, error) {
	return collectFromAllNodes[ACL, *ACL](ctx, c, "ACL", parseACLs)
}

func (c *Client) GetAllLoadBalancers(ctx context.Context) ([]LoadBalancer, error) {
	return collectFromAllNodes[LoadBalancer, *LoadBalancer](ctx, c, "Load_Balancer", parseLoadBalancers)
}

func (c *Client) GetAllLogicalSwitchPorts(ctx context.Context) ([]LogicalSwitchPort, error) {
	return collectFromAllNodes[LogicalSwitchPort, *LogicalSwitchPort](ctx, c, "Logical_Switch_Port", parseLogicalSwitchPorts)
}

type sourceTagged interface {
	getUUID() string
	setSource(node string)
}

func (ls *LogicalSwitch) getUUID() string      { return ls.UUID }
func (lr *LogicalRouter) getUUID() string      { return lr.UUID }
func (a *ACL) getUUID() string                 { return a.UUID }
func (lb *LoadBalancer) getUUID() string       { return lb.UUID }
func (lsp *LogicalSwitchPort) getUUID() string { return lsp.UUID }

func (ls *LogicalSwitch) setSource(node string)      { ls.SourceNode = node }
func (lr *LogicalRouter) setSource(node string)      { lr.SourceNode = node }
func (a *ACL) setSource(node string)                 { a.SourceNode = node }
func (lb *LoadBalancer) setSource(node string)       { lb.SourceNode = node }
func (lsp *LogicalSwitchPort) setSource(node string) { lsp.SourceNode = node }

func collectFromAllNodes[T any, PT interface {
	*T
	sourceTagged
}](ctx context.Context, c *Client, table string, parser func(string) ([]T, error)) ([]T, error) {
	pods := c.topology.NodePods
	if len(pods) == 0 {
		return nil, fmt.Errorf("no ovnkube-node pods available")
	}

	seen := make(map[string]bool)
	var result []T
	for _, pod := range pods {
		out, err := c.NBCtlOnNode(ctx, pod.NodeName, "--format=json", "list", table)
		if err != nil {
			continue
		}
		items, err := parser(out)
		if err != nil {
			continue
		}
		for i := range items {
			p := PT(&items[i])
			p.setSource(pod.NodeName)
			if !seen[p.getUUID()] {
				seen[p.getUUID()] = true
				result = append(result, items[i])
			}
		}
	}
	return result, nil
}

func (c *Client) GetNBDBStatus(ctx context.Context) (string, error) {
	return c.AppCtl(ctx, "nbdb", "-t", "/var/run/ovn/ovnnb_db.ctl", "ovsdb-server/get-db-storage-status", "OVN_Northbound")
}

func (c *Client) GetSBDBStatus(ctx context.Context) (string, error) {
	return c.AppCtl(ctx, "sbdb", "-t", "/var/run/ovn/ovnsb_db.ctl", "ovsdb-server/get-db-storage-status", "OVN_Southbound")
}

type ovsdbResult struct {
	Data     [][]any  `json:"data"`
	Headings []string `json:"headings"`
}

func parseOVSDBJSON(data string) (*ovsdbResult, error) {
	var result ovsdbResult
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("parsing OVSDB JSON: %w", err)
	}
	return &result, nil
}

func colIndex(headings []string, name string) int {
	for i, h := range headings {
		if h == name {
			return i
		}
	}
	return -1
}

func asString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%d", int(val))
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	default:
		return 0
	}
}

func asMap(v any) map[string]string {
	m := make(map[string]string)
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 {
		return m
	}
	tag, _ := arr[0].(string)
	if tag != "map" {
		return m
	}
	pairs, ok := arr[1].([]any)
	if !ok {
		return m
	}
	for _, pair := range pairs {
		kv, ok := pair.([]any)
		if !ok || len(kv) != 2 {
			continue
		}
		m[asString(kv[0])] = asString(kv[1])
	}
	return m
}

func asSet(v any) []string {
	switch val := v.(type) {
	case string:
		return []string{val}
	case []any:
		if len(val) == 2 {
			tag, _ := val[0].(string)
			if tag == "set" {
				items, ok := val[1].([]any)
				if ok {
					var result []string
					for _, item := range items {
						result = append(result, asString(item))
					}
					return result
				}
			}
			if tag == "uuid" {
				return []string{asString(val[1])}
			}
		}
		var result []string
		for _, item := range val {
			result = append(result, asString(item))
		}
		return result
	default:
		return nil
	}
}

func asUUID(v any) string {
	arr, ok := v.([]any)
	if ok && len(arr) == 2 {
		tag, _ := arr[0].(string)
		if tag == "uuid" {
			return asString(arr[1])
		}
	}
	return asString(v)
}

func parseNBGlobal(data string) (*NBGlobal, error) {
	result, err := parseOVSDBJSON(data)
	if err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no NB_Global record found")
	}
	row := result.Data[0]
	h := result.Headings

	opts := asMap(row[colIndex(h, "options")])
	nbg := &NBGlobal{
		UUID:          asUUID(row[colIndex(h, "_uuid")]),
		Name:          asString(row[colIndex(h, "name")]),
		NorthdVersion: opts["northd_internal_version"],
		NBCfg:         asInt(row[colIndex(h, "nb_cfg")]),
		SBCfg:         asInt(row[colIndex(h, "sb_cfg")]),
		HVCfg:         asInt(row[colIndex(h, "hv_cfg")]),
		Options:       opts,
	}
	return nbg, nil
}

func parseLogicalSwitches(data string) ([]LogicalSwitch, error) {
	result, err := parseOVSDBJSON(data)
	if err != nil {
		return nil, err
	}
	var switches []LogicalSwitch
	for _, row := range result.Data {
		h := result.Headings
		ls := LogicalSwitch{
			UUID:  asUUID(row[colIndex(h, "_uuid")]),
			Name:  asString(row[colIndex(h, "name")]),
			Ports: asSet(row[colIndex(h, "ports")]),
		}
		switches = append(switches, ls)
	}
	return switches, nil
}

func parseLogicalRouters(data string) ([]LogicalRouter, error) {
	result, err := parseOVSDBJSON(data)
	if err != nil {
		return nil, err
	}
	var routers []LogicalRouter
	for _, row := range result.Data {
		h := result.Headings
		lr := LogicalRouter{
			UUID:  asUUID(row[colIndex(h, "_uuid")]),
			Name:  asString(row[colIndex(h, "name")]),
			Ports: asSet(row[colIndex(h, "ports")]),
		}
		routers = append(routers, lr)
	}
	return routers, nil
}

func parseACLs(data string) ([]ACL, error) {
	result, err := parseOVSDBJSON(data)
	if err != nil {
		return nil, err
	}
	var acls []ACL
	for _, row := range result.Data {
		h := result.Headings
		acl := ACL{
			UUID:        asUUID(row[colIndex(h, "_uuid")]),
			Action:      asString(row[colIndex(h, "action")]),
			Direction:   asString(row[colIndex(h, "direction")]),
			Match:       asString(row[colIndex(h, "match")]),
			Priority:    asInt(row[colIndex(h, "priority")]),
			ExternalIDs: asMap(row[colIndex(h, "external_ids")]),
		}
		nameIdx := colIndex(h, "name")
		if nameIdx >= 0 {
			acl.Name = asString(row[nameIdx])
		}
		acls = append(acls, acl)
	}
	return acls, nil
}

func parseLoadBalancers(data string) ([]LoadBalancer, error) {
	result, err := parseOVSDBJSON(data)
	if err != nil {
		return nil, err
	}
	var lbs []LoadBalancer
	for _, row := range result.Data {
		h := result.Headings
		lb := LoadBalancer{
			UUID:        asUUID(row[colIndex(h, "_uuid")]),
			Name:        asString(row[colIndex(h, "name")]),
			ExternalIDs: asMap(row[colIndex(h, "external_ids")]),
		}
		protoIdx := colIndex(h, "protocol")
		if protoIdx >= 0 {
			lb.Protocol = asString(row[protoIdx])
		}
		vipsIdx := colIndex(h, "vips")
		if vipsIdx >= 0 {
			lb.VIPs = asMap(row[vipsIdx])
		}
		lbs = append(lbs, lb)
	}
	return lbs, nil
}

func parseLogicalSwitchPorts(data string) ([]LogicalSwitchPort, error) {
	result, err := parseOVSDBJSON(data)
	if err != nil {
		return nil, err
	}
	var ports []LogicalSwitchPort
	for _, row := range result.Data {
		h := result.Headings
		lsp := LogicalSwitchPort{
			UUID:         asUUID(row[colIndex(h, "_uuid")]),
			Name:         asString(row[colIndex(h, "name")]),
			Addresses:    asSet(row[colIndex(h, "addresses")]),
			Type:         asString(row[colIndex(h, "type")]),
			ExternalIDs:  asMap(row[colIndex(h, "external_ids")]),
			PortSecurity: asSet(row[colIndex(h, "port_security")]),
		}
		ports = append(ports, lsp)
	}
	return ports, nil
}

func (c *Client) NBCtlShow(ctx context.Context) (string, error) {
	return c.NBCtl(ctx, "show")
}

func (c *Client) GetNorthdStatus(ctx context.Context) (string, error) {
	out, err := c.ExecInNodePod(ctx, c.topology.NodePods[0].NodeName, "northd", []string{"ls", "/var/run/ovn/"})
	if err != nil {
		return "unknown", nil
	}
	if strings.Contains(out, "ovn-northd.pid") {
		return "running", nil
	}
	return "not running", nil
}
