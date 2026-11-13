package clientroute

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
)

type happProfileJSON struct {
	Name              string            `json:"Name"`
	GlobalProxy       string            `json:"GlobalProxy"`
	RemoteDNSType     string            `json:"RemoteDNSType"`
	RemoteDNSDomain   string            `json:"RemoteDNSDomain"`
	RemoteDNSIP       string            `json:"RemoteDNSIP"`
	DomesticDNSType   string            `json:"DomesticDNSType"`
	DomesticDNSDomain string            `json:"DomesticDNSDomain"`
	DomesticDNSIP     string            `json:"DomesticDNSIP"`
	Geoipurl          string            `json:"Geoipurl"`
	Geositeurl        string            `json:"Geositeurl"`
	LastUpdated       string            `json:"LastUpdated"`
	DNSHosts          map[string]string `json:"DnsHosts"`
	DirectSites       []string          `json:"DirectSites"`
	DirectIP          []string          `json:"DirectIp"`
	ProxySites        []string          `json:"ProxySites"`
	ProxyIP           []string          `json:"ProxyIp"`
	BlockSites        []string          `json:"BlockSites"`
	BlockIP           []string          `json:"BlockIp"`
	DomainStrategy    string            `json:"DomainStrategy"`
	FakeDNS           string            `json:"FakeDNS"`
	UseChunkFiles     string            `json:"UseChunkFiles"`
}

var happPrivateIPs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "224.0.0.0/4", "255.255.255.255"}

func renderHapp(in Input, rules []Resolved) (string, error) {
	p := happProfileJSON{
		Name:              in.Title,
		GlobalProxy:       "true",
		RemoteDNSType:     "DoH",
		RemoteDNSDomain:   "https://cloudflare-dns.com/dns-query",
		RemoteDNSIP:       "1.1.1.1",
		DomesticDNSType:   "DoU",
		DomesticDNSIP:     "77.88.8.8",
		Geoipurl:          GeoipURL,
		Geositeurl:        GeositeURL,
		LastUpdated:       strconv.FormatInt(in.UpdatedAt.Unix(), 10),
		DNSHosts:          map[string]string{"cloudflare-dns.com": "1.1.1.1"},
		DirectSites:       []string{},
		DirectIP:          append([]string{}, happPrivateIPs...),
		ProxySites:        []string{},
		ProxyIP:           []string{},
		BlockSites:        []string{},
		BlockIP:           []string{},
		DomainStrategy:    "IPIfNonMatch",
		FakeDNS:           "false",
		UseChunkFiles:     "true",
	}
	for _, r := range rules {
		sites, ips := &p.ProxySites, &p.ProxyIP
		switch r.Action {
		case ActionDirect:
			sites, ips = &p.DirectSites, &p.DirectIP
		case ActionBlock:
			sites, ips = &p.BlockSites, &p.BlockIP
		}
		*sites = uniqueAppend(*sites, r.Domain...)
		*ips = uniqueAppend(*ips, r.IP...)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return "happ://routing/onadd/" + base64.StdEncoding.EncodeToString(raw), nil
}
