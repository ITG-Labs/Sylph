package role

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"strings"

	"github.com/ITG-Labs/sylph/deploy/prod"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/envfile"
)

type Kind int

const (
	Text Kind = iota
	Secret
	Bool
	Choice
)

type Field struct {
	Key         string
	Title       string
	Description string
	Kind        Kind
	Choices     []string
	Optional    bool
	Advanced    bool
	Toggle      string
	Example     bool
	Validate    func(string) error
}

type Toggle struct {
	Name        string
	Title       string
	Description string
}

type Role struct {
	Name        string
	Title       string
	DomainKey   string
	TokenKey    string
	CertReaders []string
	Admins      bool
	Toggles     []Toggle
	Fields      []Field
	check       func(*envfile.Env) []error
}

const (
	KeyVersion  = "SYLPH_VERSION"
	KeyCoreURL  = "SYLPH_CORE_URL"
	KeyCoreName = "SYLPH_CORE_NAME"
	KeyPort     = "SYLPH_HTTPS_PORT"
	KeyPassword = "POSTGRES_PASSWORD"

	DefaultCoreName = "core.sylph.internal"
)

var (
	coreURL = Field{
		Key:         KeyCoreURL,
		Title:       "Адрес ядра",
		Description: "IP или домен машины ядра и порт: https://203.0.113.10:443",
		Example:     true,
		Validate:    CoreURL,
	}
	coreName = Field{
		Key:         KeyCoreName,
		Title:       "Имя в серте ядра",
		Description: "DNS для него не нужен; менять, только если выпускал серт ядра на другое имя",
		Advanced:    true,
		Validate:    Hostname,
	}
	httpsPort = Field{
		Key:         KeyPort,
		Title:       "Внешний порт",
		Description: "наружу торчит только он",
		Advanced:    true,
		Validate:    Port,
	}
	subnet = Field{
		Key:         "SYLPH_SUBNET",
		Title:       "Подсеть compose",
		Description: "менять, только если пересекается с сетями машины",
		Advanced:    true,
		Validate:    IPv4Subnet,
	}
	caddyIP = Field{
		Key:         "SYLPH_CADDY_IP",
		Title:       "Адрес Caddy в подсети",
		Description: "сервис верит X-Forwarded-For только от него",
		Advanced:    true,
		Validate:    IPv4,
	}
	logLevel = Field{
		Key:      "SYLPH_LOG_LEVEL",
		Title:    "Уровень логов",
		Kind:     Choice,
		Choices:  []string{"debug", "info", "warn", "error"},
		Advanced: true,
	}
)

func tz(key string, desc string) Field {
	return Field{Key: key, Title: "Часовой пояс", Description: desc, Validate: TimeZone}
}

func domain(key string, title string, desc string) Field {
	return Field{Key: key, Title: title, Description: desc, Example: true, Validate: Domain}
}

var all = []*Role{
	{
		Name:        "core",
		Title:       "core — ядро, Postgres и крон (машина A)",
		CertReaders: []string{"core", "cron"},
		Toggles: []Toggle{{
			Name:        "yookassa",
			Title:       "Подключить оплату ЮKassa?",
			Description: "без неё ядро работает, а оплата в боте выключена",
		}},
		Fields: []Field{
			{Key: "SYLPH_YOOKASSA_SHOP_ID", Title: "ЮKassa: shop ID", Description: "в кабинете ЮKassa → Интеграция", Toggle: "yookassa", Validate: ShopID},
			{Key: "SYLPH_YOOKASSA_SECRET_KEY", Title: "ЮKassa: секретный ключ", Description: "live_… для боя, test_… для тестового магазина", Kind: Secret, Toggle: "yookassa", Validate: SecretKey},
			{Key: "SYLPH_YOOKASSA_RETURN_URL", Title: "ЮKassa: куда вернуть юзера", Description: "после оплаты, например https://t.me/<бот>", Toggle: "yookassa", Validate: HTTPSURL},
			{Key: "SYLPH_BOT_USERNAME", Title: "Имя клиентского бота", Description: "без @; для ссылок-приглашений в приватные тарифы, можно пусто", Optional: true, Validate: BotUsername},
			tz("SYLPH_STATS_TZ", "пояс суток статистики"),
			httpsPort,
			logLevel,
		},
		check: checkCore,
	},
	{
		Name:        "panel",
		Title:       "panel — панель админов (машина B)",
		DomainKey:   "SYLPH_PANEL_DOMAIN",
		CertReaders: []string{"panel"},
		Admins:      true,
		Fields: []Field{
			domain("SYLPH_PANEL_DOMAIN", "Домен панели", "DNS смотрит прямо на эту машину; серт выпустит Caddy"),
			coreURL,
			{Key: "SYLPH_PANEL_SESSION_HOURS", Title: "Сессия после входа, часов", Description: "от 1 до 168", Validate: Hours},
			coreName, subnet, caddyIP, httpsPort, logLevel,
		},
		check: checkCaddyIP,
	},
	{
		Name:        "bot",
		Title:       "bot — клиентский бот и вебхук ЮKassa (машина C)",
		DomainKey:   "SYLPH_BOT_DOMAIN",
		TokenKey:    "SYLPH_BOT_TOKEN",
		CertReaders: []string{"bot"},
		Fields: []Field{
			{Key: "SYLPH_BOT_TOKEN", Title: "Токен бота", Description: "от @BotFather", Kind: Secret, Validate: BotToken},
			domain("SYLPH_BOT_DOMAIN", "Домен вебхука", "DNS смотрит прямо на эту машину; в ЮKassa — https://<домен>/yookassa"),
			{Key: "SYLPH_SUBS_URL", Title: "Адрес subs", Description: "ссылка юзера — <адрес>/<токен>, например https://sub.example.com", Example: true, Validate: HTTPSURL},
			coreURL,
			{Key: "SYLPH_BOT_CUSTOM_EMOJI", Title: "Иконки из пака tgiosicons?", Description: "нужен Telegram Premium у владельца бота", Kind: Bool},
			tz("SYLPH_BOT_TZ", "пояс дат в сообщениях бота"),
			coreName, subnet, caddyIP, httpsPort, logLevel,
		},
		check: checkCaddyIP,
	},
	{
		Name:        "subs",
		Title:       "subs — подписки для клиентов (машина D)",
		DomainKey:   "SYLPH_SUBS_DOMAIN",
		CertReaders: []string{"subs"},
		Fields: []Field{
			domain("SYLPH_SUBS_DOMAIN", "Домен subs", "DNS смотрит прямо на эту машину; тот же адрес — в SYLPH_SUBS_URL бота"),
			coreURL,
			{Key: "SYLPH_SUBS_UPDATE_HOURS", Title: "Как часто клиенту обновлять подписку, часов", Description: "от 1 до 168", Validate: Hours},
			tz("SYLPH_SUBS_TZ", "пояс «оплачено до» на страничке подписки"),
			coreName, subnet, caddyIP, httpsPort, logLevel,
		},
		check: checkCaddyIP,
	},
	{
		Name:        "node",
		Title:       "node — нода: Caddy, xray и агент",
		CertReaders: []string{"agent"},
		Fields: []Field{
			coreURL,
			{Key: "XRAY_VERSION", Title: "Версия xray-core", Description: "агент пишет конфиг под неё", Validate: XrayVersion},
			coreName, httpsPort, logLevel,
		},
	},
	{
		Name:        "monitor",
		Title:       "monitor — бот алертов (машина M)",
		TokenKey:    "SYLPH_MONITOR_BOT_TOKEN",
		CertReaders: []string{"monitor"},
		Fields: []Field{
			coreURL,
			{Key: "SYLPH_MONITOR_BOT_TOKEN", Title: "Токен бота алертов", Description: "отдельный бот от @BotFather, не клиентский", Kind: Secret, Validate: BotToken},
			{Key: "SYLPH_MONITOR_CHAT_IDS", Title: "Кому слать алерты", Description: "Telegram ID через запятую; каждый потом пишет боту /start", Validate: ChatIDs},
			{Key: "SYLPH_MONITOR_PANEL_URL", Title: "Адрес панели", Description: "необязательно: в алертах появятся ссылки", Optional: true, Example: true, Validate: HTTPSURL},
			tz("SYLPH_MONITOR_TZ", "пояс времени в сообщениях"),
			coreName, logLevel,
		},
	},
}

func All() []*Role {
	return all
}

func Get(name string) (*Role, error) {
	for _, r := range all {
		if r.Name == name {
			return r, nil
		}
	}
	return nil, fmt.Errorf("нет роли %q: core, panel, bot, subs, node или monitor", name)
}

func Names() []string {
	names := make([]string, len(all))
	for i, r := range all {
		names[i] = r.Name
	}
	return names
}

func (r *Role) File(name string) ([]byte, error) {
	return fs.ReadFile(prod.Files, r.Name+"/"+name)
}

func (r *Role) DeployFiles() []string {
	files := []string{"compose.yml"}
	if _, err := fs.Stat(prod.Files, r.Name+"/Caddyfile"); err == nil {
		files = append(files, "Caddyfile")
	}
	return files
}

func (r *Role) Template() (*envfile.Env, []byte, error) {
	raw, err := r.File(".env.example")
	if err != nil {
		return nil, nil, err
	}
	env, err := envfile.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s/.env.example: %w", r.Name, err)
	}
	return env, raw, nil
}

func (r *Role) Defaults() (*envfile.Env, []byte, error) {
	env, raw, err := r.Template()
	if err != nil {
		return nil, nil, err
	}
	for _, f := range r.Fields {
		if f.Example {
			env.Set(f.Key, "")
		}
	}
	return env, raw, nil
}

func (r *Role) Field(key string) (Field, bool) {
	for _, f := range r.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

func (r *Role) ToggleOn(name string, env *envfile.Env) bool {
	for _, f := range r.Fields {
		if f.Toggle == name && env.Get(f.Key) != "" {
			return true
		}
	}
	return false
}

func (r *Role) Validate(env *envfile.Env) []error {
	var errs []error
	for _, f := range r.Fields {
		v := env.Get(f.Key)
		if err := envfile.ValidValue(v); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Key, err))
			continue
		}
		if f.Toggle != "" && !r.ToggleOn(f.Toggle, env) {
			continue
		}
		if v == "" {
			if !f.Optional && f.Kind != Bool {
				errs = append(errs, fmt.Errorf("%s: пусто", f.Key))
			}
			continue
		}
		if err := f.Check(v); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Key, err))
		}
	}
	if r.check != nil {
		errs = append(errs, r.check(env)...)
	}
	return errs
}

func (f Field) Check(v string) error {
	switch f.Kind {
	case Bool:
		if v != "true" && v != "false" {
			return errors.New("true или false")
		}
		return nil
	case Choice:
		for _, c := range f.Choices {
			if v == c {
				return nil
			}
		}
		return fmt.Errorf("одно из: %s", strings.Join(f.Choices, ", "))
	}
	if f.Validate != nil {
		return f.Validate(v)
	}
	return nil
}

func checkCore(env *envfile.Env) []error {
	if err := ValidPassword(env.Get(KeyPassword)); err != nil {
		return []error{fmt.Errorf("%s: %w", KeyPassword, err)}
	}
	return nil
}

func ValidPassword(s string) error {
	if len(s) < 16 {
		return errors.New("пароль базы — минимум 16 символов")
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return errors.New("пароль базы — только буквы и цифры: он идёт в DSN ядра")
		}
	}
	return nil
}

func checkCaddyIP(env *envfile.Env) []error {
	p, err1 := netip.ParsePrefix(env.Get("SYLPH_SUBNET"))
	a, err2 := netip.ParseAddr(env.Get("SYLPH_CADDY_IP"))
	if err1 != nil || err2 != nil {
		return nil
	}
	if !p.Contains(a) || a == p.Addr() {
		return []error{errors.New("SYLPH_CADDY_IP должен быть внутри SYLPH_SUBNET")}
	}
	return nil
}
