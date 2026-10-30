package agent

import "strconv"

type caddyConfig struct {
	Admin caddyAdmin `json:"admin"`
	Apps  *caddyApps `json:"apps,omitempty"`
}

type caddyAdmin struct {
	Listen string `json:"listen"`
}

type caddyApps struct {
	Layer4 caddyLayer4   `json:"layer4"`
	HTTP   *caddyHTTPApp `json:"http,omitempty"`
	TLS    *caddyTLSApp  `json:"tls,omitempty"`
}

type caddyLayer4 struct {
	Servers map[string]caddyServer `json:"servers"`
}

type caddyServer struct {
	Listen []string     `json:"listen"`
	Routes []caddyRoute `json:"routes"`
}

type caddyRoute struct {
	Match  []caddyMatch   `json:"match,omitempty"`
	Handle []caddyHandler `json:"handle"`
}

type caddyMatch struct {
	TLS caddyTLSMatch `json:"tls"`
}

type caddyTLSMatch struct {
	SNI []string `json:"sni"`
}

type caddyHandler struct {
	Handler       string          `json:"handler"`
	ProxyProtocol string          `json:"proxy_protocol"`
	Upstreams     []caddyUpstream `json:"upstreams"`
}

type caddyUpstream struct {
	Dial []string `json:"dial"`
}

type CaddyInput struct {
	Inbounds    []Inbound
	AdminListen string
	ACMECA      string
	ACMECARoot  string
}

func BuildCaddy(inbounds []Inbound, adminListen string) ([]byte, error) {
	return BuildCaddyWith(CaddyInput{Inbounds: inbounds, AdminListen: adminListen})
}

func BuildCaddyWith(in CaddyInput) ([]byte, error) {
	sorted, err := sortedInbounds(in.Inbounds)
	if err != nil {
		return nil, err
	}
	cfg := caddyConfig{Admin: caddyAdmin{Listen: in.AdminListen}}
	servers := map[string]caddyServer{}
	fallback := map[string]int{}
	fronted := map[string]bool{}
	var order []string
	for i, ib := range sorted {
		internal := firstInternalPort + i
		name := "p" + strconv.Itoa(ib.Port)
		srv, ok := servers[name]
		if !ok {
			srv.Listen = []string{":" + strconv.Itoa(ib.Port)}
			order = append(order, name)
		}
		if _, ok := fallback[name]; !ok && ib.Security == "reality" {
			fallback[name] = internal
		}
		switch {
		case ib.TLS == nil:
			srv.Routes = append(srv.Routes, caddyRoute{
				Match:  []caddyMatch{{TLS: caddyTLSMatch{SNI: ib.Reality.ServerNames}}},
				Handle: proxyTo(internal),
			})
		case !fronted[ib.TLS.Domain]:
			fronted[ib.TLS.Domain] = true
			srv.Routes = append(srv.Routes, caddyRoute{
				Match:  []caddyMatch{{TLS: caddyTLSMatch{SNI: []string{ib.TLS.Domain}}}},
				Handle: proxyTo(tlsFrontPort),
			})
		}
		servers[name] = srv
	}
	for _, name := range order {
		if port, ok := fallback[name]; ok {
			srv := servers[name]
			srv.Routes = append(srv.Routes, caddyRoute{Handle: proxyTo(port)})
			servers[name] = srv
		}
	}
	if len(servers) > 0 {
		cfg.Apps = &caddyApps{Layer4: caddyLayer4{Servers: servers}}
		if sites := tlsSites(sorted); len(sites) > 0 {
			cfg.Apps.HTTP, cfg.Apps.TLS = buildTLSFront(sites, in)
		}
	}
	return marshalConfig(cfg)
}

func proxyTo(port int) []caddyHandler {
	return []caddyHandler{{
		Handler:       "proxy",
		ProxyProtocol: "v2",
		Upstreams:     []caddyUpstream{{Dial: []string{"127.0.0.1:" + strconv.Itoa(port)}}},
	}}
}
