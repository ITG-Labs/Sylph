package bot

import (
	"errors"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

var defaultYooKassaIPs = []string{
	"185.71.76.0/27",
	"185.71.77.0/27",
	"77.75.153.0/25",
	"77.75.156.11/32",
	"77.75.156.35/32",
	"77.75.154.128/25",
	"2a02:5180::/32",
}

type Config struct {
	Token          string
	CoreURL        string
	TLSCert        string
	TLSKey         string
	CACert         string
	ServerName     string
	SubsURL        string
	TelegramAPI    string
	WebhookListen  string
	TrustedProxies []netip.Prefix
	YooKassaIPs    []netip.Prefix
	Location       *time.Location
	CustomEmoji    bool
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Token:         os.Getenv("SYLPH_BOT_TOKEN"),
		CoreURL:       os.Getenv("SYLPH_CORE_URL"),
		TLSCert:       os.Getenv("SYLPH_TLS_CERT"),
		TLSKey:        os.Getenv("SYLPH_TLS_KEY"),
		CACert:        os.Getenv("SYLPH_CA_CERT"),
		ServerName:    envOr("SYLPH_CORE_NAME", "core.sylph.internal"),
		SubsURL:       strings.TrimRight(os.Getenv("SYLPH_SUBS_URL"), "/"),
		TelegramAPI:   strings.TrimRight(envOr("SYLPH_TELEGRAM_API", "https://api.telegram.org"), "/"),
		WebhookListen: os.Getenv("SYLPH_BOT_WEBHOOK_LISTEN"),
	}
	if cfg.Token == "" {
		return nil, errors.New("SYLPH_BOT_TOKEN is required")
	}
	if cfg.CoreURL == "" {
		return nil, errors.New("SYLPH_CORE_URL is required")
	}
	if cfg.TLSCert == "" || cfg.TLSKey == "" || cfg.CACert == "" {
		return nil, errors.New("SYLPH_TLS_CERT, SYLPH_TLS_KEY and SYLPH_CA_CERT are required")
	}
	if u, err := url.Parse(cfg.SubsURL); err != nil || !httpScheme(u) || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("SYLPH_SUBS_URL must be an http(s) URL without query")
	}
	if u, err := url.Parse(cfg.TelegramAPI); err != nil || !httpScheme(u) {
		return nil, errors.New("SYLPH_TELEGRAM_API must be an http(s) URL")
	}
	var err error
	if cfg.TrustedProxies, err = prefixes("SYLPH_BOT_TRUSTED_PROXIES", os.Getenv("SYLPH_BOT_TRUSTED_PROXIES")); err != nil {
		return nil, err
	}
	allow := os.Getenv("SYLPH_BOT_YOOKASSA_IPS")
	if allow == "" {
		allow = strings.Join(defaultYooKassaIPs, ",")
	}
	if cfg.YooKassaIPs, err = prefixes("SYLPH_BOT_YOOKASSA_IPS", allow); err != nil {
		return nil, err
	}
	if cfg.Location, err = time.LoadLocation(envOr("SYLPH_BOT_TZ", "Europe/Moscow")); err != nil {
		return nil, errors.New("SYLPH_BOT_TZ must be an IANA time zone")
	}
	switch os.Getenv("SYLPH_BOT_CUSTOM_EMOJI") {
	case "", "0", "false":
	case "1", "true":
		cfg.CustomEmoji = true
	default:
		return nil, errors.New("SYLPH_BOT_CUSTOM_EMOJI must be true or false")
	}
	return cfg, nil
}

func httpScheme(u *url.URL) bool {
	return (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func prefixes(key, list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			addr, err := netip.ParseAddr(item)
			if err != nil {
				return nil, errors.New(key + " must be a comma-separated list of IPs or CIDRs")
			}
			out = append(out, netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, errors.New(key + " must be a comma-separated list of IPs or CIDRs")
		}
		out = append(out, netip.PrefixFrom(p.Addr().Unmap(), p.Bits()).Masked())
	}
	return out, nil
}

func inAny(list []netip.Prefix, addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range list {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
