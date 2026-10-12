package subs

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goldenConnections() []Connection {
	return []Connection{
		{
			Name: "Нидерланды · Main", Address: "nl1.example.com", Port: 443,
			UUID: "091192b5-1fd3-4aeb-9733-810ef24c2964", Flow: "xtls-rprx-vision",
			Transport: "tcp", Security: "reality", SNI: "www.apple.com",
			PublicKey: "EznkPne5ZLOrLvDbqseDQycq-ca9v1kEytFwD1-fy3s", ShortID: "98b7a90e659d766f", Fingerprint: "chrome",
		},
		{
			Name: "Финляндия", Address: "2001:db8::1", Port: 443,
			UUID: "091192b5-1fd3-4aeb-9733-810ef24c2964", Flow: "xtls-rprx-vision",
			Transport: "tcp", Security: "reality", SNI: "fi.example.org",
			PublicKey: "s4KAfPmX6ufQ_n4VYWN2xG7mUVATAqaL0yY0GcmjGw0", ShortID: "1234", Fingerprint: "firefox",
		},
	}
}

func goldenStub() []Connection {
	return []Connection{stubConnection("⛔ Подписка истекла — продли в боте", "6f1d3c4e-8f43-4a5c-9a2b-0c8d7e6f5a4b")}
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("SYLPH_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from golden:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func decodeBase64(t *testing.T, body []byte) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		t.Fatalf("body is not standard base64: %v", err)
	}
	return raw
}

func TestRenderBase64MatchesGolden(t *testing.T) {
	assertGolden(t, "links_two.txt", decodeBase64(t, RenderBase64(goldenConnections())))
	assertGolden(t, "links_stub.txt", decodeBase64(t, RenderBase64(goldenStub())))
}

func TestVlessLinkCarriesEveryRealityField(t *testing.T) {
	links := strings.Split(string(decodeBase64(t, RenderBase64(goldenConnections()))), "\n")
	if len(links) != 2 {
		t.Fatalf("links = %q", links)
	}
	u, err := url.Parse(links[1])
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "vless" || u.User.Username() != "091192b5-1fd3-4aeb-9733-810ef24c2964" || u.Hostname() != "2001:db8::1" || u.Port() != "443" {
		t.Fatalf("link = %s", links[1])
	}
	if u.Fragment != "Финляндия" {
		t.Fatalf("name = %q", u.Fragment)
	}
	want := map[string]string{
		"type": "tcp", "security": "reality", "encryption": "none", "flow": "xtls-rprx-vision",
		"sni": "fi.example.org", "fp": "firefox", "pbk": "s4KAfPmX6ufQ_n4VYWN2xG7mUVATAqaL0yY0GcmjGw0", "sid": "1234",
		"support-x25519mlkem768": "true",
	}
	q := u.Query()
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

func TestRenderClashMatchesGolden(t *testing.T) {
	got, err := RenderClash(goldenConnections())
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "clash_two.yaml", got)
	got, err = RenderClash(goldenStub())
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "clash_stub.yaml", got)
}

func TestRenderSingBoxMatchesGolden(t *testing.T) {
	got, err := RenderSingBox(goldenConnections())
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "singbox_two.json", got)
	got, err = RenderSingBox(goldenStub())
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "singbox_stub.json", got)
}

func TestProfilesKeepNamesClearOfGroupsAndBuiltins(t *testing.T) {
	conns := goldenConnections()
	conns[0].Name = groupAuto
	conns[1].Name = "DIRECT"
	clash, err := RenderClash(conns)
	if err != nil {
		t.Fatal(err)
	}
	singbox, err := RenderSingBox(append(conns, Connection{Name: "direct", Address: "x.example", Port: 443, Transport: "tcp", Security: "reality"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: " + groupAuto + " 2", "name: DIRECT 2"} {
		if !strings.Contains(string(clash), want) {
			t.Errorf("clash profile lacks %q:\n%s", want, clash)
		}
	}
	for _, want := range []string{`"tag": "` + groupAuto + ` 2"`, `"tag": "direct 2"`} {
		if !strings.Contains(string(singbox), want) {
			t.Errorf("sing-box profile lacks %q:\n%s", want, singbox)
		}
	}
	if conns[0].Name != groupAuto {
		t.Fatal("rendering renamed the caller's connections")
	}
}

func TestVlessLinkNameSurvivesFormDecoding(t *testing.T) {
	c := goldenConnections()[0]
	c.Name = "NL+DE #1 & co"
	link := string(decodeBase64(t, RenderBase64([]Connection{c})))
	fragment := link[strings.Index(link, "#")+1:]
	if strings.ContainsAny(fragment, "+ #&") {
		t.Fatalf("fragment %q keeps characters that form decoders mangle", fragment)
	}
	for _, decode := range []func(string) (string, error){url.PathUnescape, url.QueryUnescape} {
		if name, err := decode(fragment); err != nil || name != c.Name {
			t.Fatalf("decoded %q, %v", name, err)
		}
	}
}
