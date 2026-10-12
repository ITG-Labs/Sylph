package subs

import (
	"fmt"
	"strings"
)

type Format string

const (
	FormatBase64  Format = "base64"
	FormatSingBox Format = "singbox"
	FormatClash   Format = "clash"
)

var singBoxAgents = []string{"sfa", "sfi", "sfm", "sft", "sing-box"}

var clashAgents = []string{"clash", "mihomo", "flclash", "stash", "koala-clash"}

func DetectFormat(forced string, userAgent string) (Format, error) {
	switch Format(forced) {
	case FormatBase64, FormatSingBox, FormatClash:
		return Format(forced), nil
	case "":
	default:
		return "", fmt.Errorf("unknown format %q", forced)
	}
	ua := strings.ToLower(userAgent)
	if hasAnyPrefix(ua, singBoxAgents) {
		return FormatSingBox, nil
	}
	if hasAnyPrefix(ua, clashAgents) {
		return FormatClash, nil
	}
	return FormatBase64, nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
