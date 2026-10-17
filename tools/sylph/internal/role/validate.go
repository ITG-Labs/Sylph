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
		return errors.New("нужен домен")
	}
	if len(s) > 253 {
		return errors.New("слишком длинный домен")
	}
	if strings.ToLower(s) != s {
		return errors.New("домен — маленькими буквами")
	}
	if net.ParseIP(s) != nil {
		return errors.New("нужен домен, а не IP: на него Caddy выпустит серт Let's Encrypt")
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return errors.New("домен с точкой, например panel.example.com")
	}
	for _, l := range labels {
		if !labelPattern.MatchString(l) {
			return fmt.Errorf("неверная часть домена %q", l)
		}
	}
	return nil
}

func Hostname(s string) error {
	if net.ParseIP(s) != nil {
		return nil
	}
	if s == "" {
		return errors.New("нужно имя")
	}
	for _, l := range strings.Split(s, ".") {
		if !labelPattern.MatchString(l) {
			return fmt.Errorf("неверная часть имени %q", l)
		}
	}
	return nil
}

func HTTPSURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("нужна ссылка вида https://адрес")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("без ?параметров, #якорей и логина в ссылке")
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
		return errors.New("адрес ядра: https://<IP или домен машины A>:443")
	}
	u, _ := url.Parse(s)
	if u.Path != "" && u.Path != "/" {
		return errors.New("адрес ядра без пути: https://<IP или домен>:443")
	}
	return nil
}

func Port(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("порт — число от 1 до 65535")
	}
	return nil
}

func Hours(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 168 {
		return errors.New("часы — число от 1 до 168")
	}
	return nil
}

func TimeZone(s string) error {
	if s == "" || s == "Local" {
		return errors.New("нужен пояс, например Europe/Moscow")
	}
	if _, err := time.LoadLocation(s); err != nil {
		return errors.New("нет такого пояса, пример: Europe/Moscow")
	}
	return nil
}

func BotToken(s string) error {
	if !tokenPattern.MatchString(s) {
		return errors.New("токен от @BotFather выглядит как 123456789:AAE…")
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
			return fmt.Errorf("%q — не Telegram ID (нужны числа через запятую)", part)
		}
		n++
	}
	if n == 0 {
		return errors.New("нужен хотя бы один Telegram ID")
	}
	return nil
}

func ShopID(s string) error {
	if !shopIDPattern.MatchString(s) {
		return errors.New("shop ID — только цифры")
	}
	return nil
}

func SecretKey(s string) error {
	if !secretKeyPattern.MatchString(s) {
		return errors.New("секретный ключ ЮKassa начинается с live_ или test_")
	}
	return nil
}

func BotUsername(s string) error {
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "@") {
		return errors.New("имя бота без @")
	}
	if !botUsernamePattern.MatchString(s) || !strings.HasSuffix(strings.ToLower(s), "bot") {
		return errors.New("имя бота — латиница, цифры и _, кончается на bot")
	}
	return nil
}

func XrayVersion(s string) error {
	if !versionPattern.MatchString(s) {
		return errors.New("версия xray вида 26.10.10")
	}
	return nil
}

func IPv4Subnet(s string) error {
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() || p.Masked() != p {
		return errors.New("подсеть IPv4 вида 172.30.10.0/24")
	}
	return nil
}

func IPv4(s string) error {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return errors.New("адрес IPv4 вида 172.30.10.10")
	}
	return nil
}

func AdminName(s string) error {
	if s == "" || strings.TrimSpace(s) != s {
		return errors.New("имя без пробелов по краям")
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 64 {
		return errors.New("имя — до 64 символов")
	}
	for _, c := range s {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' || c == ',' {
			return errors.New("имя без запятых и управляющих символов")
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
			return nil, fmt.Errorf("имя %q повторяется", part)
		}
		seen[part] = true
		names = append(names, part)
	}
	return names, nil
}
