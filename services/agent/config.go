package agent

import (
	"errors"
	"os"
	"time"
)

const (
	defaultXrayAPI    = "127.0.0.1:10085"
	defaultXrayConfig = "/etc/xray/config.json"
	defaultCaddyAdmin = "unix//run/caddy-admin/admin.sock|0666"
	defaultGeoDir     = "/usr/local/share/xray/sylph"
	defaultGeoPrefix  = "sylph"
)

type Config struct {
	CoreURL       string
	TLSCert       string
	TLSKey        string
	CACert        string
	ServerName    string
	CachePath     string
	StatsPath     string
	NodeStatePath string
	ListenAPI     string
	XrayVersion   string
	XrayAPI       string
	XrayTimeout   time.Duration
	XrayConfig    string
	CaddyAdmin    string
	GeoDir        string
	GeoStatePath  string
	XrayGeoPrefix string
	SyncInterval  time.Duration
	Egress        []EgressAddr
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		CoreURL:       os.Getenv("SYLPH_CORE_URL"),
		TLSCert:       os.Getenv("SYLPH_TLS_CERT"),
		TLSKey:        os.Getenv("SYLPH_TLS_KEY"),
		CACert:        os.Getenv("SYLPH_CA_CERT"),
		ServerName:    envOr("SYLPH_CORE_NAME", "core.sylph.internal"),
		CachePath:     envOr("SYLPH_CACHE_PATH", "cache.json"),
		StatsPath:     envOr("SYLPH_STATS_PATH", "stats.json"),
		NodeStatePath: envOr("SYLPH_NODE_STATE_PATH", "node.json"),
		ListenAPI:     envOr("SYLPH_LISTEN_API", ""),
		XrayVersion:   os.Getenv("SYLPH_XRAY_VERSION"),
		XrayAPI:       envOr("SYLPH_XRAY_API", defaultXrayAPI),
		XrayTimeout:   5 * time.Second,
		XrayConfig:    envOr("SYLPH_XRAY_CONFIG", defaultXrayConfig),
		CaddyAdmin:    envOr("SYLPH_CADDY_ADMIN", defaultCaddyAdmin),
		GeoDir:        envOr("SYLPH_GEO_DIR", defaultGeoDir),
		GeoStatePath:  envOr("SYLPH_GEO_STATE_PATH", "geo.json"),
		XrayGeoPrefix: envOr("SYLPH_XRAY_GEO_PREFIX", defaultGeoPrefix),
		SyncInterval:  30 * time.Second,
	}
	if cfg.CoreURL == "" {
		return nil, errors.New("SYLPH_CORE_URL is required")
	}
	egress, err := ParseEgress(os.Getenv("SYLPH_EGRESS"))
	if err != nil {
		return nil, errors.New("SYLPH_EGRESS: " + err.Error())
	}
	cfg.Egress = egress
	if cfg.TLSCert == "" || cfg.TLSKey == "" || cfg.CACert == "" {
		return nil, errors.New("SYLPH_TLS_CERT, SYLPH_TLS_KEY and SYLPH_CA_CERT are required")
	}
	if v := os.Getenv("SYLPH_SYNC_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, errors.New("SYLPH_SYNC_INTERVAL: " + err.Error())
		}
		cfg.SyncInterval = d
	}
	if v := os.Getenv("SYLPH_XRAY_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, errors.New("SYLPH_XRAY_TIMEOUT: " + err.Error())
		}
		if d <= 0 {
			return nil, errors.New("SYLPH_XRAY_TIMEOUT must be positive")
		}
		cfg.XrayTimeout = d
	}
	return cfg, nil
}

func LoadBootstrapConfig() *Config {
	return &Config{
		XrayAPI:    envOr("SYLPH_XRAY_API", defaultXrayAPI),
		XrayConfig: envOr("SYLPH_XRAY_CONFIG", defaultXrayConfig),
	}
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
