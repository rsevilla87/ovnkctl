package ovn

import (
	"encoding/json"
	"fmt"
	"net"
)

type PodNetworkInfo struct {
	IPAddress  string
	MACAddress string
	GatewayIP  string
}

type podNetworkAnnotation struct {
	IPAddresses []string `json:"ip_addresses"`
	MACAddress  string   `json:"mac_address"`
	GatewayIPs  []string `json:"gateway_ips"`
	IPAddress   string   `json:"ip_address"`
	GatewayIP   string   `json:"gateway_ip"`
}

func ParsePodNetworkAnnotation(annotation string) (*PodNetworkInfo, error) {
	var networks map[string]podNetworkAnnotation
	if err := json.Unmarshal([]byte(annotation), &networks); err != nil {
		return nil, fmt.Errorf("parsing pod network annotation: %w", err)
	}
	nw, ok := networks["default"]
	if !ok {
		for _, v := range networks {
			nw = v
			break
		}
	}
	ip := nw.IPAddress
	if ip == "" && len(nw.IPAddresses) > 0 {
		ip = nw.IPAddresses[0]
	}
	ipObj, _, err := net.ParseCIDR(ip)

	if err != nil {
		return nil, fmt.Errorf("parsing IP address: %w", err)
	}
	ip = ipObj.String()
	gw := nw.GatewayIP
	if gw == "" && len(nw.GatewayIPs) > 0 {
		gw = nw.GatewayIPs[0]
	}
	return &PodNetworkInfo{
		IPAddress:  ip,
		MACAddress: nw.MACAddress,
		GatewayIP:  gw,
	}, nil
}

func IPToMAC(ipStr string) (string, error) {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return "", fmt.Errorf("invalid IPv4 address: %s", ipStr)
	}
	return fmt.Sprintf("0a:58:%02x:%02x:%02x:%02x", ip[0], ip[1], ip[2], ip[3]), nil
}
