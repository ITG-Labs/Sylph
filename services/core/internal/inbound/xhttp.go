package inbound

import (
	"math"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/netaddr"
)

const (
	TransportXHTTP = "xhttp"
	SecurityTLS    = "tls"
	ModeAuto       = "auto"
	ModePacketUp   = "packet-up"
)

var (
	XHTTPModes = []string{ModeAuto, ModePacketUp, "stream-up", "stream-one"}
	ALPNs      = []string{"h3", "h2", "http/1.1"}

	paddingPlacements = []string{"", "cookie", "header", "query", "queryInHeader"}
	paddingMethods    = []string{"", "repeat-x", "tokenish"}
	metaPlacements    = []string{"", "path", "cookie", "header", "query"}
	uplinkPlacements  = []string{"", "auto", "body", "cookie", "header"}

	sessionTables = map[string]int{
		"ALPHABET": 26, "Alphabet": 52, "BASE36": 36, "Base62": 62, "HEX": 16,
		"alphabet": 26, "base36": 36, "hex": 16, "number": 10,
	}

	pathPattern   = regexp.MustCompile(`^/[A-Za-z0-9._~!$&'()+,;=:@/-]*$`)
	keyPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	headerPattern = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]{1,64}$")
	methodPattern = regexp.MustCompile(`^[A-Za-z]{1,16}$`)
	rangePattern  = regexp.MustCompile(`^(\d{1,10})(?:-(\d{1,10}))?$`)
)

var unroutable = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func unroutableHost(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range unroutable {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

type Range string

type Xmux struct {
	MaxConcurrency   Range `json:"maxConcurrency,omitempty"`
	MaxConnections   Range `json:"maxConnections,omitempty"`
	CMaxReuseTimes   Range `json:"cMaxReuseTimes,omitempty"`
	HMaxRequestTimes Range `json:"hMaxRequestTimes,omitempty"`
	HMaxReusableSecs Range `json:"hMaxReusableSecs,omitempty"`
	HKeepAlivePeriod int64 `json:"hKeepAlivePeriod,omitempty"`
}

type Extra struct {
	Headers              map[string]string `json:"headers,omitempty"`
	XPaddingBytes        Range             `json:"xPaddingBytes,omitempty"`
	XPaddingObfsMode     bool              `json:"xPaddingObfsMode,omitempty"`
	XPaddingKey          string            `json:"xPaddingKey,omitempty"`
	XPaddingHeader       string            `json:"xPaddingHeader,omitempty"`
	XPaddingPlacement    string            `json:"xPaddingPlacement,omitempty"`
	XPaddingMethod       string            `json:"xPaddingMethod,omitempty"`
	UplinkHTTPMethod     string            `json:"uplinkHTTPMethod,omitempty"`
	SessionIDPlacement   string            `json:"sessionIDPlacement,omitempty"`
	SessionIDKey         string            `json:"sessionIDKey,omitempty"`
	SessionIDTable       string            `json:"sessionIDTable,omitempty"`
	SessionIDLength      Range             `json:"sessionIDLength,omitempty"`
	SeqPlacement         string            `json:"seqPlacement,omitempty"`
	SeqKey               string            `json:"seqKey,omitempty"`
	UplinkDataPlacement  string            `json:"uplinkDataPlacement,omitempty"`
	UplinkDataKey        string            `json:"uplinkDataKey,omitempty"`
	UplinkChunkSize      Range             `json:"uplinkChunkSize,omitempty"`
	NoGRPCHeader         bool              `json:"noGRPCHeader,omitempty"`
	NoSSEHeader          bool              `json:"noSSEHeader,omitempty"`
	ScMaxEachPostBytes   Range             `json:"scMaxEachPostBytes,omitempty"`
	ScMinPostsIntervalMs Range             `json:"scMinPostsIntervalMs,omitempty"`
	ScMaxBufferedPosts   int64             `json:"scMaxBufferedPosts,omitempty"`
	ScStreamUpServerSecs Range             `json:"scStreamUpServerSecs,omitempty"`
	ServerMaxHeaderBytes int32             `json:"serverMaxHeaderBytes,omitempty"`
	Xmux                 *Xmux             `json:"xmux,omitempty"`
}

type XHTTP struct {
	Path       string `json:"path"`
	Mode       string `json:"mode"`
	ClientMode string `json:"client_mode"`
	Extra      Extra  `json:"extra"`
}

type TLS struct {
	Domain      string   `json:"domain"`
	Decoy       string   `json:"decoy"`
	Address     string   `json:"address"`
	Port        int      `json:"port"`
	SNI         string   `json:"sni"`
	Host        string   `json:"host"`
	ALPN        []string `json:"alpn"`
	Fingerprint string   `json:"fingerprint"`
}

type Endpoint struct {
	Address string
	Port    int
	SNI     string
	Host    string
}

func (t *TLS) Client() Endpoint {
	e := Endpoint{Address: t.Address, Port: t.Port, SNI: t.SNI, Host: t.Host}
	if e.Address == "" {
		e.Address = t.Domain
	}
	if e.SNI == "" {
		e.SNI = t.Domain
		if net.ParseIP(e.Address) == nil {
			e.SNI = e.Address
		}
	}
	if e.Host == "" {
		e.Host = e.SNI
	}
	return e
}

func (x *XHTTP) LinkExtra() Extra {
	e := x.Extra
	e.NoSSEHeader, e.ScMaxBufferedPosts, e.ScStreamUpServerSecs, e.ServerMaxHeaderBytes = false, 0, "", 0
	return e
}

func PathPrefix(path string) string {
	if strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}

func PathsOverlap(a, b string) bool {
	a, b = PathPrefix(a), PathPrefix(b)
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func SNINames(s *Spec) []string {
	if s.TLS != nil {
		return []string{s.TLS.Domain}
	}
	return ServerNames(s)
}

func normalizeTransport(s *Spec) error {
	if s.Transport == TransportTCP {
		if s.XHTTP != nil {
			return fieldErr("xhttp", "is only for transport %s", TransportXHTTP)
		}
		return nil
	}
	if s.XHTTP == nil {
		return fieldErr("xhttp", "is required for transport %s", TransportXHTTP)
	}
	return normalizeXHTTP(s.XHTTP, s.Security)
}

func ClientModeInEffect(clientMode, security string) string {
	if clientMode != ModeAuto {
		return clientMode
	}
	if security == SecurityReality {
		return "stream-one"
	}
	return ModePacketUp
}

func serverTakes(server, client string) bool {
	switch server {
	case ModeAuto:
		return true
	case "stream-up":
		return client == "stream-up" || client == "stream-one"
	default:
		return server == client
	}
}

func normalizeXHTTP(x *XHTTP, security string) error {
	if len(x.Path) > 200 || !pathPattern.MatchString(x.Path) {
		return fieldErr("xhttp.path", "must start with / and hold only URL path characters, at most 200")
	}
	for _, m := range []struct {
		field string
		value *string
	}{{"xhttp.mode", &x.Mode}, {"xhttp.client_mode", &x.ClientMode}} {
		if *m.value == "" {
			*m.value = ModeAuto
		}
		if !slices.Contains(XHTTPModes, *m.value) {
			return fieldErr(m.field, "must be one of %s", strings.Join(XHTTPModes, ", "))
		}
	}
	if eff := ClientModeInEffect(x.ClientMode, security); !serverTakes(x.Mode, eff) {
		return fieldErr("xhttp.client_mode", "client mode %s (%s in effect) is refused by server mode %s", x.ClientMode, eff, x.Mode)
	}
	packetUp := x.Mode == ModePacketUp && x.ClientMode == ModePacketUp
	return normalizeExtra(&x.Extra, packetUp)
}

func normalizeExtra(e *Extra, packetUp bool) error {
	const f = "xhttp.extra."
	if len(e.Headers) > 32 {
		return fieldErr(f+"headers", "at most 32")
	}
	for k, v := range e.Headers {
		if !headerPattern.MatchString(k) || strings.EqualFold(k, "host") {
			return fieldErr(f+"headers", "%q is not an allowed header name", k)
		}
		if len(v) > 512 || strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fieldErr(f+"headers", "value of %s must be at most 512 characters without control characters", k)
		}
	}
	if err := checkRange(f+"xPaddingBytes", e.XPaddingBytes, 1); err != nil {
		return err
	}
	for _, c := range []struct {
		field, value string
		allowed      []string
	}{
		{"xPaddingPlacement", e.XPaddingPlacement, paddingPlacements},
		{"xPaddingMethod", e.XPaddingMethod, paddingMethods},
		{"sessionIDPlacement", e.SessionIDPlacement, metaPlacements},
		{"seqPlacement", e.SeqPlacement, metaPlacements},
		{"uplinkDataPlacement", e.UplinkDataPlacement, uplinkPlacements},
	} {
		if !slices.Contains(c.allowed, c.value) {
			return fieldErr(f+c.field, "must be one of %s", strings.Join(c.allowed[1:], ", "))
		}
	}
	for _, c := range []struct{ field, value string }{
		{"xPaddingKey", e.XPaddingKey}, {"sessionIDKey", e.SessionIDKey}, {"seqKey", e.SeqKey}, {"uplinkDataKey", e.UplinkDataKey},
	} {
		if c.value != "" && !keyPattern.MatchString(c.value) {
			return fieldErr(f+c.field, "must be 1-64 characters of A-Z, a-z, 0-9, _ . -")
		}
	}
	if e.XPaddingHeader != "" && !headerPattern.MatchString(e.XPaddingHeader) {
		return fieldErr(f+"xPaddingHeader", "must be an HTTP header name")
	}
	if (e.UplinkDataPlacement == "cookie" || e.UplinkDataPlacement == "header") && !packetUp {
		return fieldErr(f+"uplinkDataPlacement", "%s works only when server and client mode are %s", e.UplinkDataPlacement, ModePacketUp)
	}
	if e.UplinkHTTPMethod != "" {
		if !methodPattern.MatchString(e.UplinkHTTPMethod) {
			return fieldErr(f+"uplinkHTTPMethod", "must be an HTTP method")
		}
		e.UplinkHTTPMethod = strings.ToUpper(e.UplinkHTTPMethod)
		if e.UplinkHTTPMethod == "GET" && !packetUp {
			return fieldErr(f+"uplinkHTTPMethod", "GET works only when server and client mode are %s", ModePacketUp)
		}
	}
	if err := checkSessionTable(e); err != nil {
		return err
	}
	for _, c := range []struct {
		field string
		value Range
	}{
		{"uplinkChunkSize", e.UplinkChunkSize}, {"scMaxEachPostBytes", e.ScMaxEachPostBytes},
		{"scMinPostsIntervalMs", e.ScMinPostsIntervalMs}, {"scStreamUpServerSecs", e.ScStreamUpServerSecs},
	} {
		if err := checkRange(f+c.field, c.value, 0); err != nil {
			return err
		}
	}
	if e.ScMaxBufferedPosts < 0 {
		return fieldErr(f+"scMaxBufferedPosts", "must not be negative")
	}
	if e.ServerMaxHeaderBytes < 0 {
		return fieldErr(f+"serverMaxHeaderBytes", "must not be negative")
	}
	if e.Xmux != nil {
		return checkXmux(e.Xmux)
	}
	return nil
}

const maxSessionIDLength = 128

func checkSessionTable(e *Extra) error {
	const f = "xhttp.extra."
	if err := checkRange(f+"sessionIDLength", e.SessionIDLength, 0); err != nil {
		return err
	}
	if _, to, _ := rangeBounds(e.SessionIDLength); to > maxSessionIDLength {
		return fieldErr(f+"sessionIDLength", "must be at most %d", maxSessionIDLength)
	}
	if e.SessionIDTable == "" {
		return nil
	}
	size, ok := sessionTables[e.SessionIDTable]
	if !ok {
		if len(e.SessionIDTable) > 256 || strings.ContainsFunc(e.SessionIDTable, func(r rune) bool { return r < 0x21 || r > 0x7e }) {
			return fieldErr(f+"sessionIDTable", "must be a predefined table or at most 256 printable ASCII characters")
		}
		size = len(e.SessionIDTable)
	}
	from, to, _ := rangeBounds(e.SessionIDLength)
	if from <= 0 {
		return fieldErr(f+"sessionIDLength", "must be set and above 0 when sessionIDTable is set")
	}
	room, term, base, need := new(big.Int), new(big.Int), big.NewInt(int64(size)), big.NewInt(2<<30)
	for k := from; k <= to && room.Cmp(need) < 0; k++ {
		room.Add(room, term.Exp(base, big.NewInt(k), nil))
	}
	if room.Cmp(need) < 0 {
		return fieldErr(f+"sessionIDTable", "table and length give fewer than 2^31 session ids")
	}
	return nil
}

func checkXmux(x *Xmux) error {
	const f = "xhttp.extra.xmux."
	for _, c := range []struct {
		field string
		value Range
	}{
		{"maxConcurrency", x.MaxConcurrency}, {"maxConnections", x.MaxConnections}, {"cMaxReuseTimes", x.CMaxReuseTimes},
		{"hMaxRequestTimes", x.HMaxRequestTimes}, {"hMaxReusableSecs", x.HMaxReusableSecs},
	} {
		if err := checkRange(f+c.field, c.value, 0); err != nil {
			return err
		}
	}
	_, concurrency, _ := rangeBounds(x.MaxConcurrency)
	_, connections, _ := rangeBounds(x.MaxConnections)
	if concurrency > 0 && connections > 0 {
		return fieldErr("xhttp.extra.xmux", "maxConcurrency and maxConnections cannot be set together")
	}
	if x.HKeepAlivePeriod < -1 {
		return fieldErr(f+"hKeepAlivePeriod", "must be -1 or more")
	}
	return nil
}

func rangeBounds(r Range) (from, to int64, ok bool) {
	if r == "" {
		return 0, 0, true
	}
	m := rangePattern.FindStringSubmatch(string(r))
	if m == nil {
		return 0, 0, false
	}
	from, _ = strconv.ParseInt(m[1], 10, 64)
	to = from
	if m[2] != "" {
		to, _ = strconv.ParseInt(m[2], 10, 64)
	}
	return from, to, to <= math.MaxInt32 && from <= to
}

func checkRange(field string, r Range, min int64) error {
	from, _, ok := rangeBounds(r)
	if !ok || (r != "" && from < min) {
		return fieldErr(field, "must be a number or a range like 100-1000, at least %d", min)
	}
	return nil
}

func normalizeTLS(t *TLS) error {
	t.Domain = strings.TrimSuffix(strings.ToLower(t.Domain), ".")
	if net.ParseIP(t.Domain) != nil || !netaddr.PublicHost(t.Domain) {
		return fieldErr("tls.domain", "must be a public domain name")
	}
	decoy, err := normalizeDecoy(t.Decoy)
	if err != nil {
		return err
	}
	t.Decoy = decoy
	t.Address = netaddr.CanonicalHost(strings.TrimSuffix(t.Address, "."))
	if t.Address != "" && !netaddr.PublicHost(t.Address) {
		return fieldErr("tls.address", "must be a public IP or domain name")
	}
	if t.Port == 0 {
		t.Port = PublicPort
	}
	if t.Port < 1 || t.Port > 65535 {
		return fieldErr("tls.port", "must be 1-65535")
	}
	for _, h := range []struct {
		field string
		value *string
	}{{"tls.sni", &t.SNI}, {"tls.host", &t.Host}} {
		*h.value = strings.TrimSuffix(strings.ToLower(*h.value), ".")
		if *h.value != "" && (net.ParseIP(*h.value) != nil || !netaddr.IsHostname(*h.value) || !strings.Contains(*h.value, ".")) {
			return fieldErr(h.field, "must be a domain name")
		}
	}
	for i, a := range t.ALPN {
		if !slices.Contains(ALPNs, a) || slices.Contains(t.ALPN[:i], a) {
			return fieldErr("tls.alpn", "each must be one of %s, listed once", strings.Join(ALPNs, ", "))
		}
	}
	if slices.Contains(t.ALPN, "h3") && (t.Address == "" || t.Address == t.Domain) {
		return fieldErr("tls.alpn", "h3 works only through a CDN: the node itself does not serve QUIC")
	}
	if t.Fingerprint == "" {
		t.Fingerprint = "chrome"
	}
	if !slices.Contains(Fingerprints, t.Fingerprint) {
		return fieldErr("tls.fingerprint", "must be one of %s", strings.Join(Fingerprints, ", "))
	}
	return nil
}

func DecoyHost(decoy string) string {
	u, err := url.Parse(decoy)
	if err != nil {
		return ""
	}
	return netaddr.CanonicalHost(strings.TrimSuffix(u.Hostname(), "."))
}

func normalizeDecoy(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	bad := fieldErr("tls.decoy", "must be http:// or https:// and a public site, without path, query or login")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Opaque != "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", bad
	}
	host := netaddr.CanonicalHost(strings.TrimSuffix(u.Hostname(), "."))
	if !netaddr.PublicHost(host) || unroutableHost(host) {
		return "", bad
	}
	if port := u.Port(); port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return "", bad
		}
		host = net.JoinHostPort(host, strconv.Itoa(p))
	} else if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}
