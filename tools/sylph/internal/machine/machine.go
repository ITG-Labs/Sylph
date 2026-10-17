package machine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

type Level int

const (
	OK Level = iota
	Warn
	Fail
)

type Result struct {
	Level  Level
	Detail string
}

func ok(format string, args ...any) Result {
	return Result{Level: OK, Detail: fmt.Sprintf(format, args...)}
}

func warn(format string, args ...any) Result {
	return Result{Level: Warn, Detail: fmt.Sprintf(format, args...)}
}

func fail(format string, args ...any) Result {
	return Result{Level: Fail, Detail: fmt.Sprintf(format, args...)}
}

func Root() error {
	if os.Geteuid() != 0 {
		return errors.New("нужен root: запусти через sudo")
	}
	return nil
}

func PortFree(port string) Result {
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fail("порт %s занят другой программой", port)
	}
	l.Close()
	return ok("порт %s свободен", port)
}

func TimeSync(ctx context.Context) Result {
	out, err := exec.CommandContext(ctx, "timedatectl", "show", "-p", "NTPSynchronized", "--value").Output()
	if err != nil {
		return ok("timedatectl нет — синхронизацию часов не проверить")
	}
	if strings.TrimSpace(string(out)) == "yes" {
		return ok("часы синхронизированы")
	}
	return warn("часы не синхронизированы по NTP: mTLS и сроки сертов зависят от времени")
}

func DiskFree(dir string) Result {
	for {
		var st syscall.Statfs_t
		if err := syscall.Statfs(dir, &st); err == nil {
			free := st.Bavail * uint64(st.Bsize)
			gb := float64(free) / (1 << 30)
			if free < 2<<30 {
				return warn("свободно %.1f ГБ — мало, база и логи растут", gb)
			}
			return ok("свободно %.1f ГБ", gb)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ok("место не проверить")
		}
		dir = parent
	}
}

func LocalIPs() []netip.Addr {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []netip.Addr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || virtualInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr()
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || !ip.IsGlobalUnicast() {
				continue
			}
			ips = append(ips, ip)
		}
	}
	return ips
}

func virtualInterface(name string) bool {
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "tailscale", "wg", "tun"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

var LookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func DNS(ctx context.Context, domain string, local []netip.Addr) []Result {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := LookupIP(ctx, domain)
	if err != nil || len(ips) == 0 {
		return []Result{warn("%s не резолвится: заведи A-запись на эту машину, иначе Let's Encrypt не выпустит серт", domain)}
	}
	var v4, v6 []string
	match := false
	for _, ip := range ips {
		ip = ip.Unmap()
		if ip.Is4() {
			v4 = append(v4, ip.String())
			if slices.Contains(local, ip) {
				match = true
			}
		} else {
			v6 = append(v6, ip.String())
		}
	}
	var results []Result
	switch {
	case len(v4) == 0:
		results = append(results, warn("у %s нет A-записи", domain))
	case match:
		results = append(results, ok("%s → %s, это эта машина", domain, strings.Join(v4, ", ")))
	default:
		var mine []string
		for _, ip := range local {
			if ip.Is4() {
				mine = append(mine, ip.String())
			}
		}
		results = append(results, warn("%s → %s, а у машины %s; если провайдер делает NAT — ок", domain, strings.Join(v4, ", "), strings.Join(mine, ", ")))
	}
	if len(v6) > 0 {
		results = append(results, warn("у %s есть AAAA (%s): по раннбуку AAAA не заводим — IPv6-клиенты придут с адреса шлюза Docker", domain, strings.Join(v6, ", ")))
	}
	return results
}

type ClientCert struct {
	CA      []byte
	CertPEM []byte
	KeyPEM  []byte
}

func ProbeCore(ctx context.Context, coreURL string, coreName string, cc ClientCert) Result {
	cert, err := tls.X509KeyPair(cc.CertPEM, cc.KeyPEM)
	if err != nil {
		return fail("серт из пакета не читается: %v", err)
	}
	ca, err := mtls.ParseCertPEM(cc.CA)
	if err != nil {
		return fail("ca.pem из пакета не читается")
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
			ServerName:   coreName,
			MinVersion:   tls.VersionTLS13,
		}},
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(coreURL, "/")+"/v1/sylph-install-probe", nil)
	if err != nil {
		return fail("адрес ядра: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return probeError(err, coreURL)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fail("ядро не приняло серт (HTTP %d): отозван или не той роли", resp.StatusCode)
	}
	return ok("ядро на %s пускает с этим сертом", coreURL)
}

func probeError(err error, coreURL string) Result {
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var netErr net.Error
	msg := err.Error()
	switch {
	case errors.As(err, &unknown):
		return fail("серт ядра на %s подписан другим CA — пакет от другого CA?", coreURL)
	case errors.As(err, &hostname):
		return fail("серт ядра выпущен не на SYLPH_CORE_NAME: %v", hostname.Error())
	case strings.Contains(msg, "bad certificate"), strings.Contains(msg, "certificate required"), strings.Contains(msg, "unknown certificate authority"):
		return fail("ядро не приняло серт: он от другого CA или отозван")
	case errors.As(err, &netErr) && netErr.Timeout():
		return warn("ядро на %s не ответило за 8 с — ещё не поднято или закрыт порт", coreURL)
	}
	return warn("ядро на %s недоступно (%s) — ещё не поднято?", coreURL, shortNetError(err))
}

func shortNetError(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return opErr.Err.Error()
	}
	return "нет соединения"
}

var TelegramAPI = "https://api.telegram.org"

func TelegramGetMe(ctx context.Context, token string) Result {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, TelegramAPI+"/bot"+token+"/getMe", nil)
	if err != nil {
		return fail("токен не годится для запроса")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return warn("Telegram недоступен, токен не проверить")
	}
	defer resp.Body.Close()
	var body struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound:
		return fail("Telegram не знает этот токен: перепроверь у @BotFather")
	case decodeErr == nil && body.OK && body.Result.Username != "":
		return ok("бот @%s", body.Result.Username)
	}
	return warn("Telegram ответил HTTP %d, токен не проверить", resp.StatusCode)
}
