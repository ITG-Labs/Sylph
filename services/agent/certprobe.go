package agent

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"sync"
	"time"
)

const certProbeTimeout = 3 * time.Second

type CertStatus struct {
	NotAfter *time.Time `json:"not_after,omitempty"`
	Error    string     `json:"error,omitempty"`
}

func tlsDomains(inbounds []Inbound) map[string]int {
	domains := map[string]int{}
	for _, in := range inbounds {
		if in.TLS != nil {
			domains[in.TLS.Domain] = in.Port
		}
	}
	return domains
}

func probeCerts(ctx context.Context, domains map[string]int) map[string]CertStatus {
	out := make(map[string]CertStatus, len(domains))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for domain, port := range domains {
		wg.Go(func() {
			st := probeCert(ctx, domain, port)
			mu.Lock()
			out[domain] = st
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

func probeCert(ctx context.Context, domain string, port int) CertStatus {
	ctx, cancel := context.WithTimeout(ctx, certProbeTimeout)
	defer cancel()
	d := tls.Dialer{Config: &tls.Config{ServerName: domain, InsecureSkipVerify: true, NextProtos: []string{"h2", "http/1.1"}}}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return CertStatus{Error: truncate("no certificate: "+err.Error(), 300)}
	}
	defer func() { _ = conn.Close() }()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return CertStatus{Error: "no certificate"}
	}
	if err := certs[0].VerifyHostname(domain); err != nil {
		return CertStatus{Error: truncate(err.Error(), 300)}
	}
	notAfter := certs[0].NotAfter.UTC()
	return CertStatus{NotAfter: &notAfter}
}
