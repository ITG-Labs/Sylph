package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	tlsFrontPort = 20000
	edgeHeader   = "X-Sylph-Edge"
	stubPage     = "<!doctype html><html><head><meta charset=\"utf-8\"><title>Welcome</title></head><body><h1>Welcome</h1><p>This site is under construction.</p></body></html>"
)

var (
	xhttpModes      = []string{"auto", "packet-up", "stream-up", "stream-one"}
	xhttpPath       = regexp.MustCompile(`^/[A-Za-z0-9._~!$&'()*+,;=:@%/-]*$`)
	xrayHeaders     = []string{"X-Padding", "Via", "Access-Control-Allow-Credentials", "Access-Control-Allow-Origin", "X-Accel-Buffering"}
	xhttpExtraNames = []string{
		"headers", "xPaddingBytes", "xPaddingObfsMode", "xPaddingKey", "xPaddingHeader", "xPaddingPlacement", "xPaddingMethod",
		"uplinkHTTPMethod", "sessionIDPlacement", "sessionIDKey", "sessionIDTable", "sessionIDLength", "seqPlacement", "seqKey",
		"uplinkDataPlacement", "uplinkDataKey", "uplinkChunkSize", "noGRPCHeader", "noSSEHeader", "scMaxEachPostBytes",
		"scMinPostsIntervalMs", "scMaxBufferedPosts", "scStreamUpServerSecs", "serverMaxHeaderBytes", "xmux",
	}
)

type XHTTP struct {
	Path       string          `json:"path"`
	Mode       string          `json:"mode"`
	ClientMode string          `json:"client_mode"`
	Extra      json.RawMessage `json:"extra,omitempty"`
}

type TLS struct {
	Domain string `json:"domain"`
	Decoy  string `json:"decoy"`
}

func checkXHTTP(x *XHTTP) error {
	if len(x.Path) > 200 || !xhttpPath.MatchString(x.Path) {
		return fmt.Errorf("xhttp path %q is not a plain URL path", x.Path)
	}
	if !slices.Contains(xhttpModes, x.Mode) {
		return fmt.Errorf("xhttp mode %q is not supported", x.Mode)
	}
	if len(x.Extra) == 0 {
		return nil
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(x.Extra, &extra); err != nil || extra == nil {
		return errors.New("xhttp extra must be an object")
	}
	for name := range extra {
		if !slices.Contains(xhttpExtraNames, name) {
			return fmt.Errorf("xhttp extra field %q is not allowed", name)
		}
	}
	return nil
}

func checkTLS(t *TLS) error {
	if net.ParseIP(t.Domain) != nil || !publicDest(net.JoinHostPort(t.Domain, "443")) {
		return fmt.Errorf("tls domain %q must be a public domain name", t.Domain)
	}
	if t.Decoy == "" {
		return nil
	}
	u, err := url.Parse(t.Decoy)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.Fragment != "" || !publicDest(decoyDial(u)) {
		return fmt.Errorf("tls decoy %q must be a public http(s) site", t.Decoy)
	}
	if strings.EqualFold(u.Hostname(), t.Domain) {
		return fmt.Errorf("tls decoy %q points back at the domain", t.Decoy)
	}
	return nil
}

func checkSite(in Inbound, site []Inbound) error {
	for _, other := range site {
		a, b := sitePath(in.XHTTP.Path), sitePath(other.XHTTP.Path)
		if strings.HasPrefix(a, b) || strings.HasPrefix(b, a) {
			return fmt.Errorf("inbound %q: path %s overlaps %s of %q on %s", in.Tag, in.XHTTP.Path, other.XHTTP.Path, other.Tag, in.TLS.Domain)
		}
		if in.TLS.Decoy != other.TLS.Decoy {
			return fmt.Errorf("inbound %q: decoy differs from %q on %s", in.Tag, other.Tag, in.TLS.Domain)
		}
	}
	return nil
}

func sitePath(path string) string {
	if strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}

func decoyDial(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return ""
	}
	return net.JoinHostPort(u.Hostname(), port)
}

type tlsSite struct {
	domain string
	decoy  string
	paths  []sitePathRoute
}

type sitePathRoute struct {
	match string
	port  int
}

func xrayPathMatch(x *XHTTP) string {
	var meta struct {
		SessionIDPlacement string `json:"sessionIDPlacement"`
		SeqPlacement       string `json:"seqPlacement"`
	}
	if len(x.Extra) > 0 {
		_ = json.Unmarshal(x.Extra, &meta)
	}
	inPath := func(p string) bool { return p == "" || p == "path" }
	if inPath(meta.SessionIDPlacement) || inPath(meta.SeqPlacement) {
		return sitePath(x.Path) + "*"
	}
	return x.Path
}

func tlsSites(sorted []Inbound) []tlsSite {
	var sites []tlsSite
	for i, in := range sorted {
		if in.TLS == nil {
			continue
		}
		route := sitePathRoute{match: xrayPathMatch(in.XHTTP), port: firstInternalPort + i}
		j := slices.IndexFunc(sites, func(s tlsSite) bool { return s.domain == in.TLS.Domain })
		if j < 0 {
			sites = append(sites, tlsSite{domain: in.TLS.Domain, decoy: in.TLS.Decoy})
			j = len(sites) - 1
		}
		sites[j].paths = append(sites[j].paths, route)
	}
	return sites
}

type caddyHTTPApp struct {
	Servers map[string]caddyHTTPServer `json:"servers"`
}

type caddyHTTPServer struct {
	Listen                []string         `json:"listen"`
	ListenerWrappers      []map[string]any `json:"listener_wrappers"`
	TLSConnectionPolicies []map[string]any `json:"tls_connection_policies"`
	AutomaticHTTPS        map[string]any   `json:"automatic_https"`
	Protocols             []string         `json:"protocols"`
	Routes                []map[string]any `json:"routes"`
}

type caddyTLSApp struct {
	Certificates map[string]any `json:"certificates"`
	Automation   map[string]any `json:"automation"`
}

func buildTLSFront(sites []tlsSite, in CaddyInput) (*caddyHTTPApp, *caddyTLSApp) {
	domains := make([]string, 0, len(sites))
	routes := make([]map[string]any, 0, len(sites))
	for _, s := range sites {
		domains = append(domains, s.domain)
		inner := make([]map[string]any, 0, len(s.paths)+2)
		inner = append(inner, map[string]any{"handle": []map[string]any{{"handler": "headers", "response": map[string]any{"delete": xrayHeaders, "deferred": true}}}})
		for _, p := range s.paths {
			inner = append(inner, map[string]any{
				"match":  []map[string]any{{"path": []string{p.match}}},
				"handle": []map[string]any{xrayProxy(p.port, s.decoy)},
			})
		}
		inner = append(inner, map[string]any{"handle": []map[string]any{decoyHandler(s.decoy, false)}})
		routes = append(routes, map[string]any{
			"match":    []map[string]any{{"vars": map[string][]string{"{http.request.tls.server_name}": {s.domain}}}},
			"handle":   []map[string]any{{"handler": "subroute", "routes": inner}},
			"terminal": true,
		})
	}
	server := caddyHTTPServer{
		Listen:                []string{"127.0.0.1:" + strconv.Itoa(tlsFrontPort)},
		ListenerWrappers:      []map[string]any{{"wrapper": "proxy_protocol", "allow": []string{"127.0.0.1/32"}}, {"wrapper": "tls"}},
		TLSConnectionPolicies: []map[string]any{{"match": map[string]any{"sni": domains}}},
		AutomaticHTTPS:        map[string]any{"disable_redirects": true},
		Protocols:             []string{"h1", "h2"},
		Routes:                routes,
	}
	issuer := map[string]any{"module": "acme", "challenges": map[string]any{"http": map[string]any{"disabled": true}}}
	if in.ACMECA != "" {
		issuer["ca"] = in.ACMECA
	}
	if in.ACMECARoot != "" {
		issuer["trusted_roots_pem_files"] = []string{in.ACMECARoot}
	}
	tlsApp := &caddyTLSApp{
		Certificates: map[string]any{"automate": domains},
		Automation:   map[string]any{"policies": []map[string]any{{"subjects": domains, "issuers": []map[string]any{issuer}}}},
	}
	return &caddyHTTPApp{Servers: map[string]caddyHTTPServer{"sylph-tls": server}}, tlsApp
}

func xrayProxy(port int, decoy string) map[string]any {
	return map[string]any{
		"handler":        "reverse_proxy",
		"upstreams":      []map[string]any{{"dial": "127.0.0.1:" + strconv.Itoa(port)}},
		"transport":      map[string]any{"protocol": "http", "versions": []string{"h2c"}},
		"flush_interval": -1,
		"headers":        map[string]any{"request": map[string]any{"set": map[string][]string{edgeHeader: {"1"}}}},
		"handle_response": []map[string]any{{
			"match":  map[string]any{"status_code": []int{4, 5}},
			"routes": []map[string]any{{"handle": []map[string]any{decoyHandler(decoy, true)}}},
		}},
	}
}

func decoyHandler(decoy string, rewrite bool) map[string]any {
	u, err := url.Parse(decoy)
	if decoy == "" || err != nil {
		return map[string]any{
			"handler":     "static_response",
			"status_code": 200,
			"headers":     map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
			"body":        stubPage,
		}
	}
	h := map[string]any{
		"handler":   "reverse_proxy",
		"upstreams": []map[string]any{{"dial": decoyDial(u)}},
		"headers":   map[string]any{"request": map[string]any{"set": map[string][]string{"Host": {u.Host}}}},
	}
	if u.Scheme == "https" {
		h["transport"] = map[string]any{"protocol": "http", "tls": map[string]any{"server_name": u.Hostname()}}
	}
	if rewrite {
		h["rewrite"] = map[string]any{"method": "GET", "uri": "/"}
	}
	return h
}
