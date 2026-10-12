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
	u := url.URL{
		Scheme:   "vless",
		User:     url.User(c.UUID),
		Host:     net.JoinHostPort(c.Address, strconv.Itoa(c.Port)),
		RawQuery: q.Encode(),
		Fragment: c.Name,
	}
	return u.String()
}
