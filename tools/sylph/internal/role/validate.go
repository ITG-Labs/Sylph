package role

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	labelPattern       = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	tokenPattern       = regexp.MustCompile(`^[0-9]{5,}:[A-Za-z0-9_-]{30,}$`)
	shopIDPattern      = regexp.MustCompile(`^[0-9]{1,12}$`)
	secretKeyPattern   = regexp.MustCompile(`^(live|test)_[A-Za-z0-9_-]{10,}$`)
	botUsernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)
	versionPattern     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

func Domain(s string) error {
	if s == "" {
		return errors.New("укажите домен")
	}
	if len(s) > 253 {
		return errors.New("слишком длинный домен")
	}
	if strings.ToLower(s) != s {
		return errors.New("домен указывается строчными буквами")
	}
	if net.ParseIP(s) != nil {
		return errors.New("укажите домен, а не IP-адрес: для него Caddy выпустит сертификат Let's Encrypt")
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return errors.New("домен должен содержать точку, например panel.example.com")
	}
	for _, l := range labels {
		if !labelPattern.MatchString(l) {
			return fmt.Errorf("недопустимая часть домена %q", l)
		}
	}
	return nil
}

func Hostname(s string) error {
	if net.ParseIP(s) != nil {
		return nil
	}
	if s == "" {
		return errors.New("укажите имя")
	}
	for _, l := range strings.Split(s, ".") {
		if !labelPattern.MatchString(l) {
			return fmt.Errorf("недопустимая часть имени %q", l)
		}
	}
	return nil
}

func HTTPSURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("укажите ссылку вида https://адрес")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("ссылка не должна содержать ?параметры, #якоря и логин")
	}
	if err := Hostname(u.Hostname()); err != nil {
		return err
	}
	if p := u.Port(); p != "" {
		return Port(p)
	}
	return nil
}

func CoreURL(s string) error {
	if err := HTTPSURL(s); err != nil {
		return errors.New("адрес ядра в формате https://<IP или домен сервера ядра>:443")
	}
	u, _ := url.Parse(s)
	if u.Path != "" && u.Path != "/" {
		return errors.New("адрес ядра указывается без пути: https://<IP или домен>:443")
	}
	return nil
}

func Port(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("порт должен быть числом от 1 до 65535")
	}
	return nil
}

func Hours(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 168 {
		return errors.New("количество часов должно быть числом от 1 до 168")
	}
	return nil
}

func TimeZone(s string) error {
	if s == "" || s == "Local" {
		return errors.New("укажите часовой пояс, например Europe/Moscow")
	}
	if _, err := time.LoadLocation(s); err != nil {
		return errors.New("часовой пояс не найден, пример: Europe/Moscow")
	}
	return nil
}

func BotToken(s string) error {
	if !tokenPattern.MatchString(s) {
		return errors.New("токен @BotFather имеет вид 123456789:AAE…")
	}
	return nil
}

func ChatIDs(s string) error {
	n := 0
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, err := strconv.ParseInt(part, 10, 64); err != nil {
			return fmt.Errorf("%q не является Telegram ID (укажите числа через запятую)", part)
		}
		n++
	}
	if n == 0 {
		return errors.New("укажите хотя бы один Telegram ID")
	}
	return nil
}

func ShopID(s string) error {
	if !shopIDPattern.MatchString(s) {
		return errors.New("shop ID должен состоять только из цифр")
	}
	return nil
}

func SecretKey(s string) error {
	if !secretKeyPattern.MatchString(s) {
		return errors.New("секретный ключ ЮKassa должен начинаться с live_ или test_")
	}
	return nil
}

func BotUsername(s string) error {
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "@") {
		return errors.New("имя бота указывается без @")
	}
	if !botUsernamePattern.MatchString(s) || !strings.HasSuffix(strings.ToLower(s), "bot") {
		return errors.New("имя бота может содержать латинские буквы, цифры и _ и должно заканчиваться на bot")
	}
	return nil
}

func XrayVersion(s string) error {
	if !versionPattern.MatchString(s) {
		return errors.New("укажите версию xray в формате 26.10.21")
	}
	return nil
}

func IPv4Subnet(s string) error {
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() || p.Masked() != p {
		return errors.New("укажите подсеть IPv4 в формате 172.30.10.0/24")
	}
	return nil
}

func IPv4(s string) error {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return errors.New("укажите адрес IPv4 в формате 172.30.10.10")
	}
	return nil
}

func AdminName(s string) error {
	if s == "" || strings.TrimSpace(s) != s {
		return errors.New("имя не должно начинаться или заканчиваться пробелом")
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 64 {
		return errors.New("имя не должно превышать 64 символов")
	}
	for _, c := range s {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' || c == ',' {
			return errors.New("имя не должно содержать запятых и управляющих символов")
		}
	}
	return nil
}

func AdminNames(s string) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if err := AdminName(part); err != nil {
			return nil, err
		}
		if seen[part] {
			return nil, fmt.Errorf("имя %q указано повторно", part)
		}
		seen[part] = true
		names = append(names, part)
	}
	return names, nil
}
