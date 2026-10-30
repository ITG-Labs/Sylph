package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/geodata"
)

func xrayImage(t *testing.T) string {
	t.Helper()
	img := os.Getenv("SYLPH_TEST_XRAY_IMAGE")
	if img == "" {
		t.Skip("SYLPH_TEST_XRAY_IMAGE not set, run make test-xray")
	}
	return img
}

func writeGeoSite(t *testing.T, dir, name string) {
	t.Helper()
	list := &geodata.GeoSiteList{Entry: []*geodata.GeoSite{{Code: "RU", Domain: []*geodata.Domain{{Type: geodata.Domain_Domain, Value: "example.ru"}}}}}
	data, err := proto.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".dat"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func xrayTest(t *testing.T, img, geoDir string, config []byte) (string, error) {
	t.Helper()
	args := []string{"run", "--rm", "-i", "--network", "none"}
	if geoDir != "" {
		args = append(args, "-v", geoDir+":/usr/local/share/xray/sylph:ro")
	}
	args = append(args, img, "run", "-test", "-c", "stdin:")
	cmd := exec.Command("docker", args...)
	cmd.Stdin = bytes.NewReader(config)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func acceptProfile() *noderoute.Profile {
	p := routedProfile()
	p.Outbounds = append(p.Outbounds,
		noderoute.Outbound{Tag: "tro", Kind: noderoute.KindProxy, Proxy: &noderoute.Proxy{Protocol: "trojan", Address: "t.example.com", Port: 443, Password: "pw", Security: "tls"}},
		noderoute.Outbound{Tag: "ss", Kind: noderoute.KindProxy, Proxy: &noderoute.Proxy{Protocol: "shadowsocks", Address: "s.example.com", Port: 8388, Method: "aes-256-gcm", Password: "pw"}},
		noderoute.Outbound{Tag: "web", Kind: noderoute.KindProxy, Proxy: &noderoute.Proxy{Protocol: "http", Address: "h.example.com", Port: 3128, Username: "u", Password: "p", Security: "tls"}},
		noderoute.Outbound{Tag: "vtls", Kind: noderoute.KindProxy, Proxy: &noderoute.Proxy{Protocol: "vless", Address: "v.example.com", Port: 443, UUID: "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b", Security: "tls"}},
		noderoute.Outbound{Tag: "warp", Kind: noderoute.KindRaw, Raw: []byte(`{"protocol":"wireguard","settings":{"secretKey":"cHJpdmF0ZWtleXByaXZhdGVrZXlwcml2YXRla2V5cHI=","address":["172.16.0.2/32"],"peers":[{"publicKey":"cHVibGlja2V5cHVibGlja2V5cHVibGlja2V5cHVibGk=","endpoint":"engage.cloudflareclient.com:2408"}]}}`)},
	)
	p.Balancers = append(p.Balancers, noderoute.Balancer{Tag: "rr", Strategy: noderoute.StrategyRoundRobin, Outbounds: []string{"tro", "ss"}})
	p.Rules = append([]noderoute.Rule{
		{Domain: []string{"geosite:category-ads-all", "full:a.example.com", "keyword:ads", "regexp:^x\\.y$"}, To: "block"},
		{IP: []string{"geoip:ru", "1.1.1.1", "10.0.0.0/8"}, Port: "443,1000-2000", Network: "tcp", To: "rr"},
		{Inbound: []string{"alpha"}, To: "warp"},
	}, p.Rules...)
	p.Default = "best"
	return p
}

func TestLiveXrayAcceptsRoutedConfig(t *testing.T) {
	img := xrayImage(t)
	geoDir := t.TempDir()
	if err := os.Chmod(geoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGeoSite(t, geoDir, "ru-blocked")
	cfg, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "127.0.0.1:10085", Routing: acceptProfile(), ServiceKeys: []string{"6f1c2a3b-4d5e-4f60-8a7b-000000000001"}, GeoPrefix: "sylph"})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := xrayTest(t, img, geoDir, cfg); err != nil || !strings.Contains(out, "Configuration OK") {
		t.Fatalf("xray rejected the routed config: %v\n%s\n%s", err, out, cfg)
	}
	if out, err := xrayTest(t, img, "", cfg); err == nil {
		t.Fatalf("xray accepted a config whose geo file is missing:\n%s", out)
	}
	plain, err := BuildXray(goldenInbounds(), "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := xrayTest(t, img, "", plain); err != nil {
		t.Fatalf("xray rejected the plain config: %v\n%s", err, out)
	}
}

func TestLiveXrayAcceptsXHTTPInbounds(t *testing.T) {
	img := xrayImage(t)
	full := cdnInbound("full", "full.example.ru", "/full")
	full.XHTTP.Mode = "packet-up"
	full.XHTTP.Extra = json.RawMessage(`{"headers":{"X-App":"1"},"xPaddingBytes":"100-1000","xPaddingObfsMode":true,"xPaddingKey":"_ym_d","xPaddingHeader":"X-Pad","xPaddingPlacement":"header","xPaddingMethod":"tokenish","uplinkHTTPMethod":"PUT","sessionIDPlacement":"cookie","sessionIDKey":"sid","sessionIDTable":"Base62","sessionIDLength":"16-24","seqPlacement":"header","seqKey":"X-Seq","uplinkDataPlacement":"cookie","uplinkDataKey":"d","uplinkChunkSize":"1024-4096","noGRPCHeader":true,"noSSEHeader":true,"scMaxEachPostBytes":"500000-1000000","scMinPostsIntervalMs":"10-50","scMaxBufferedPosts":30,"scStreamUpServerSecs":"20-80","serverMaxHeaderBytes":16384,"xmux":{"maxConnections":"2-4","cMaxReuseTimes":"64-128","hMaxRequestTimes":"600-900","hMaxReusableSecs":"1800-3000","hKeepAlivePeriod":30}}`)
	cfg, err := BuildXray(append(xhttpSet(), full), "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := xrayTest(t, img, "", cfg); err != nil || !strings.Contains(out, "Configuration OK") {
		t.Fatalf("xray rejected the xhttp config: %v\n%s\n%s", err, out, cfg)
	}
	bad := cdnInbound("bad", "bad.example.ru", "/bad")
	bad.XHTTP.Extra = json.RawMessage(`{"uplinkDataPlacement":"cookie"}`)
	cfg, err = BuildXray([]Inbound{bad}, "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := xrayTest(t, img, "", cfg); err == nil {
		t.Fatalf("xray accepted cookie uplink outside packet-up, the check is blind:\n%s", out)
	}
}
