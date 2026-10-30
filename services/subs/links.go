package subs

import (
	"encoding/base64"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func RenderBase64(conns []Connection) []byte {
	links := make([]string, len(conns))
	for i, c := range conns {
		links[i] = vlessLink(c)
	}
	return []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n"))))
}

func vlessLink(c Connection) string {
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("type", c.Transport)
	q.Set("security", c.Security)
	if c.Flow != "" {
		q.Set("flow", c.Flow)
	}
	if c.Security == "reality" {
		q.Set("sni", c.SNI)
		q.Set("fp", c.Fingerprint)
		q.Set("pbk", c.PublicKey)
		q.Set("sid", c.ShortID)
		q.Set("support-x25519mlkem768", "true")
	}
	if c.Security == "tls" {
		q.Set("sni", c.SNI)
		q.Set("fp", c.Fingerprint)
		if len(c.ALPN) > 0 {
			q.Set("alpn", strings.Join(c.ALPN, ","))
		}
	}
	if c.Transport == "xhttp" {
		q.Set("path", c.Path)
		q.Set("mode", c.Mode)
		if c.Host != "" {
			q.Set("host", c.Host)
		}
		if len(c.Extra) > 0 {
			q.Set("extra", string(c.Extra))
		}
	}
	u := url.URL{
		Scheme:      "vless",
		User:        url.User(c.UUID),
		Host:        net.JoinHostPort(c.Address, strconv.Itoa(c.Port)),
		RawQuery:    q.Encode(),
		Fragment:    c.Name,
		RawFragment: strings.ReplaceAll(url.QueryEscape(c.Name), "+", "%20"),
	}
	return u.String()
}
