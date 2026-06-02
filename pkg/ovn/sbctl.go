package ovn

import (
	"context"
	"strings"
)

type Chassis struct {
	UUID        string
	Name        string
	Hostname    string
	Encaps      []Encap
}

type Encap struct {
	Type string
	IP   string
}

type PortBinding struct {
	UUID        string
	LogicalPort string
	Chassis     string
	Datapath    string
	MAC         []string
	Type        string
	ExternalIDs map[string]string
}

func (c *Client) SBCtlShow(ctx context.Context) (string, error) {
	return c.SBCtl(ctx, "show")
}

func (c *Client) GetChassis(ctx context.Context) ([]Chassis, error) {
	out, err := c.SBCtl(ctx, "--format=json", "list", "Chassis")
	if err != nil {
		return nil, err
	}
	return parseChassis(out)
}

func (c *Client) GetPortBindings(ctx context.Context) ([]PortBinding, error) {
	out, err := c.SBCtl(ctx, "--format=json", "list", "Port_Binding")
	if err != nil {
		return nil, err
	}
	return parsePortBindings(out)
}

func parseChassis(data string) ([]Chassis, error) {
	result, err := parseOVSDBJSON(data)
	if err != nil {
		return nil, err
	}
	var chassis []Chassis
	for _, row := range result.Data {
		h := result.Headings
		ch := Chassis{
			UUID:     asUUID(row[colIndex(h, "_uuid")]),
			Name:     asString(row[colIndex(h, "name")]),
			Hostname: asString(row[colIndex(h, "hostname")]),
		}
		chassis = append(chassis, ch)
	}
	return chassis, nil
}

func parsePortBindings(data string) ([]PortBinding, error) {
	result, err := parseOVSDBJSON(data)
	if err != nil {
		return nil, err
	}
	var bindings []PortBinding
	for _, row := range result.Data {
		h := result.Headings
		pb := PortBinding{
			UUID:        asUUID(row[colIndex(h, "_uuid")]),
			LogicalPort: asString(row[colIndex(h, "logical_port")]),
			Type:        asString(row[colIndex(h, "type")]),
		}
		chassisIdx := colIndex(h, "chassis")
		if chassisIdx >= 0 {
			pb.Chassis = asUUID(row[chassisIdx])
		}
		datapathIdx := colIndex(h, "datapath")
		if datapathIdx >= 0 {
			pb.Datapath = asUUID(row[datapathIdx])
		}
		macIdx := colIndex(h, "mac")
		if macIdx >= 0 {
			pb.MAC = asSet(row[macIdx])
		}
		extIdx := colIndex(h, "external_ids")
		if extIdx >= 0 {
			pb.ExternalIDs = asMap(row[extIdx])
		}
		bindings = append(bindings, pb)
	}
	return bindings, nil
}

func ParseSBCtlShow(raw string) []Chassis {
	var chassis []Chassis
	var current *Chassis
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Chassis ") {
			if current != nil {
				chassis = append(chassis, *current)
			}
			parts := strings.Fields(trimmed)
			name := ""
			if len(parts) > 1 {
				name = strings.Trim(parts[1], "\"")
			}
			current = &Chassis{Name: name}
		} else if current != nil {
			if strings.HasPrefix(trimmed, "hostname:") {
				current.Hostname = strings.TrimSpace(strings.TrimPrefix(trimmed, "hostname:"))
				current.Hostname = strings.Trim(current.Hostname, "\"")
			} else if strings.HasPrefix(trimmed, "Encap ") {
				parts := strings.Fields(trimmed)
				if len(parts) >= 2 {
					current.Encaps = append(current.Encaps, Encap{Type: parts[1]})
				}
			} else if strings.HasPrefix(trimmed, "ip:") {
				ip := strings.TrimSpace(strings.TrimPrefix(trimmed, "ip:"))
				ip = strings.Trim(ip, "\"")
				if len(current.Encaps) > 0 {
					current.Encaps[len(current.Encaps)-1].IP = ip
				}
			}
		}
	}
	if current != nil {
		chassis = append(chassis, *current)
	}
	return chassis
}
