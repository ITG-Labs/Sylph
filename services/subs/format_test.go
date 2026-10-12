package subs

import "testing"

func TestDetectFormatByUserAgent(t *testing.T) {
	cases := map[string]Format{
		"SFA/1.14.3 (Android 15; sing-box 1.14.3)": FormatSingBox,
		"SFI/1.14.3":                  FormatSingBox,
		"SFM/1.14.3":                  FormatSingBox,
		"SFT/1.14.3":                  FormatSingBox,
		"sing-box 1.14.3":             FormatSingBox,
		"clash-verge/v2.4.2":          FormatClash,
		"Clash/1.18.0":                FormatClash,
		"clash.meta/v1.19.32":         FormatClash,
		"ClashMetaForAndroid/2.11.18": FormatClash,
		"mihomo/1.19.32":              FormatClash,
		"FlClash/v0.8.90 clash-verge": FormatClash,
		"FlClashX/v0.3.1":             FormatClash,
		"Stash/3.1.1 Clash/1.9.0":     FormatClash,
		"koala-clash/v1.0.6":          FormatClash,
		"Happ/3.6.1":                  FormatBase64,
		"v2rayNG/1.10.24":             FormatBase64,
		"v2rayN/7.15.4":               FormatBase64,
		"Streisand/2.1":               FormatBase64,
		"v2RayTun/5.18.63":            FormatBase64,
		"HiddifyNext/2.5.7 (android) like ClashMeta v2ray sing-box": FormatBase64,
		"Mozilla/5.0": FormatBase64,
		"":            FormatBase64,
	}
	for ua, want := range cases {
		got, err := DetectFormat("", ua)
		if err != nil {
			t.Fatalf("DetectFormat(%q): %v", ua, err)
		}
		if got != want {
			t.Errorf("DetectFormat(%q) = %s, want %s", ua, got, want)
		}
	}
}

func TestDetectFormatForcedByQuery(t *testing.T) {
	cases := map[string]Format{"base64": FormatBase64, "singbox": FormatSingBox, "clash": FormatClash}
	for q, want := range cases {
		got, err := DetectFormat(q, "Happ/3.6.1")
		if err != nil || got != want {
			t.Errorf("DetectFormat(%q) = %s, %v, want %s", q, got, err, want)
		}
	}
	got, err := DetectFormat("base64", "SFA/1.14.3")
	if err != nil || got != FormatBase64 {
		t.Errorf("forced base64 for sing-box UA = %s, %v", got, err)
	}
	for _, bad := range []string{"json", "Clash", "sing-box", "yaml"} {
		if _, err := DetectFormat(bad, "Happ/3.6.1"); err == nil {
			t.Errorf("DetectFormat(%q) accepted an unknown format", bad)
		}
	}
}
