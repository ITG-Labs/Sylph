package role

import (
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/envfile"
)

func TestEveryFieldIsInTheRoleExample(t *testing.T) {
	for _, r := range All() {
		tmpl, _, err := r.Template()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range r.Fields {
			v, ok := tmpl.Lookup(f.Key)
			if !ok {
				t.Fatalf("%s: %s is not in .env.example", r.Name, f.Key)
			}
			if v != "" && !f.Example && f.Toggle == "" {
				if err := f.Check(v); err != nil {
					t.Fatalf("%s: default %s=%q does not pass its own check: %v", r.Name, f.Key, v, err)
				}
			}
		}
		if _, ok := tmpl.Lookup(KeyVersion); !ok {
			t.Fatalf("%s: no %s in .env.example", r.Name, KeyVersion)
		}
	}
}

func TestEveryRoleShipsCompose(t *testing.T) {
	for _, r := range All() {
		for _, name := range r.DeployFiles() {
			if _, err := r.File(name); err != nil {
				t.Fatalf("%s/%s: %v", r.Name, name, err)
			}
		}
	}
}

func TestDefaultsDropExampleValues(t *testing.T) {
	r, err := Get("panel")
	if err != nil {
		t.Fatal(err)
	}
	env, _, err := r.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	if env.Get("SYLPH_PANEL_DOMAIN") != "" || env.Get(KeyCoreURL) != "" {
		t.Fatalf("example values leaked: %q %q", env.Get("SYLPH_PANEL_DOMAIN"), env.Get(KeyCoreURL))
	}
	if env.Get("SYLPH_PANEL_SESSION_HOURS") != "12" {
		t.Fatalf("real default lost: %q", env.Get("SYLPH_PANEL_SESSION_HOURS"))
	}
	errs := r.Validate(env)
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	if !strings.Contains(joined, "SYLPH_PANEL_DOMAIN: пусто") || !strings.Contains(joined, "SYLPH_CORE_URL: пусто") {
		t.Fatalf("errors = %s", joined)
	}
}

func TestCoreYooKassaAllOrNone(t *testing.T) {
	r, _ := Get("core")
	env, _, _ := r.Defaults()
	env.Set(KeyPassword, strings.Repeat("a1", 16))
	if errs := r.Validate(env); len(errs) != 0 {
		t.Fatalf("no YooKassa should be fine: %v", errs)
	}
	env.Set("SYLPH_YOOKASSA_SHOP_ID", "123456")
	errs := r.Validate(env)
	if len(errs) != 2 {
		t.Fatalf("partial YooKassa errors = %v", errs)
	}
	env.Set("SYLPH_YOOKASSA_SECRET_KEY", "live_abcdefghijklmnop")
	env.Set("SYLPH_YOOKASSA_RETURN_URL", "https://t.me/sylph_bot")
	if errs := r.Validate(env); len(errs) != 0 {
		t.Fatalf("full YooKassa: %v", errs)
	}
	env.Set(KeyPassword, "short")
	if errs := r.Validate(env); len(errs) != 1 {
		t.Fatalf("bad password errors = %v", errs)
	}
}

func TestCaddyIPInsideSubnet(t *testing.T) {
	r, _ := Get("bot")
	env, _, _ := r.Defaults()
	env.Merge(mustEnv(t, "SYLPH_BOT_TOKEN=123456789:AAEabcdefghijklmnopqrstuvwxyz012345\nSYLPH_BOT_DOMAIN=pay.example.com\nSYLPH_SUBS_URL=https://sub.example.com\nSYLPH_CORE_URL=https://203.0.113.10:443\n"))
	if errs := r.Validate(env); len(errs) != 0 {
		t.Fatalf("valid bot env: %v", errs)
	}
	env.Set("SYLPH_CADDY_IP", "10.0.0.1")
	if errs := r.Validate(env); len(errs) != 1 {
		t.Fatalf("caddy outside subnet: %v", errs)
	}
}

func mustEnv(t *testing.T, s string) *envfile.Env {
	t.Helper()
	env, err := envfile.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestValidators(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) error
		good []string
		bad  []string
	}{
		{"Domain", Domain, []string{"panel.example.com", "a.b"}, []string{"", "Panel.example.com", "localhost", "1.2.3.4", "-a.com", "a_b.com"}},
		{"CoreURL", CoreURL, []string{"https://203.0.113.10:443", "https://core.example.com", "https://[2001:db8::1]:443"}, []string{"http://1.2.3.4", "https://1.2.3.4/x", "https://1.2.3.4:0", "1.2.3.4:443"}},
		{"HTTPSURL", HTTPSURL, []string{"https://t.me/sylph_bot", "https://sub.example.com"}, []string{"https://x.com/?a=1", "ftp://x.com", "https://"}},
		{"Hours", Hours, []string{"1", "168"}, []string{"0", "169", "x"}},
		{"TimeZone", TimeZone, []string{"Europe/Moscow", "UTC"}, []string{"", "Mars/Base", "Local"}},
		{"BotToken", BotToken, []string{"123456789:AAEabcdefghijklmnopqrstuvwxyz012345"}, []string{"123:abc", "token"}},
		{"ChatIDs", ChatIDs, []string{"1", "1, 2", "-100123"}, []string{"", " , ", "1,x"}},
		{"SecretKey", SecretKey, []string{"live_abcdefghijk", "test_abc-def_ghijk"}, []string{"abc", "live_"}},
		{"BotUsername", BotUsername, []string{"", "sylph_bot", "SylphBot"}, []string{"@sylph_bot", "sylph", "1bot"}},
		{"XrayVersion", XrayVersion, []string{"26.10.10"}, []string{"v26.10.10", "26.10"}},
		{"IPv4Subnet", IPv4Subnet, []string{"172.30.10.0/24"}, []string{"172.30.10.1/24", "fd00::/64", "x"}},
		{"AdminName", AdminName, []string{"Владелец", "ops"}, []string{"", " a", "a,b"}},
	}
	for _, c := range cases {
		for _, v := range c.good {
			if err := c.fn(v); err != nil {
				t.Errorf("%s(%q) = %v, want ok", c.name, v, err)
			}
		}
		for _, v := range c.bad {
			if err := c.fn(v); err == nil {
				t.Errorf("%s(%q) = ok, want error", c.name, v)
			}
		}
	}
}

func TestAdminNames(t *testing.T) {
	names, err := AdminNames(" Владелец , ops ,")
	if err != nil || len(names) != 2 || names[0] != "Владелец" || names[1] != "ops" {
		t.Fatalf("names = %v, %v", names, err)
	}
	if _, err := AdminNames("a, a"); err == nil {
		t.Fatal("duplicates should fail")
	}
}
