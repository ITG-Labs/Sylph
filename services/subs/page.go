package subs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const FormatHTML Format = "html"

const pageStyle = `
:root{color-scheme:light dark;--bg:#f2f2f7;--card:#fff;--text:#1c1c1e;--muted:#6e6e73;--accent:#0a84ff;--ok:#30a14e;--bad:#d63b30}
@media (prefers-color-scheme:dark){:root{--bg:#000;--card:#1c1c1e;--text:#f2f2f7;--muted:#98989f;--ok:#32d74b;--bad:#ff453a}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font:16px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif}
main{max-width:480px;margin:0 auto;padding:24px 16px 40px}
h1{font-size:26px;margin:0 0 16px}
h2{font-size:18px;margin:28px 0 12px}
.card{background:var(--card);border-radius:16px;padding:18px;margin-bottom:12px}
.status{font-size:20px;font-weight:600;margin:0}
.status.ok{color:var(--ok)}
.status.bad{color:var(--bad)}
.hint,.label,.stores{color:var(--muted);font-size:14px;margin:6px 0 0}
.label{margin-top:16px}
.value{margin:2px 0 0;font-weight:500}
progress{width:100%;height:8px;margin-top:8px;accent-color:var(--accent)}
.btn{display:block;text-align:center;background:var(--accent);color:#fff;text-decoration:none;font-weight:600;border:0;border-radius:12px;padding:14px;font-size:16px;width:100%;cursor:pointer}
.stores a,.support a{color:var(--accent)}
input{width:100%;font:13px ui-monospace,Menlo,monospace;padding:12px;border-radius:10px;border:1px solid var(--muted);background:transparent;color:var(--text);margin-bottom:10px}
.support{text-align:center;margin-top:24px}
`

const pageScript = `
var b=document.getElementById("copy");
if(b){b.addEventListener("click",function(){
var i=document.getElementById("link");i.select();
var done=function(){b.textContent="Скопировано ✓"};
var legacy=function(){try{document.execCommand("copy");done()}catch(e){}};
if(navigator.clipboard){navigator.clipboard.writeText(i.value).then(done,legacy)}else{legacy()}
})}
`

const pageHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="robots" content="noindex, nofollow">
<title>{{.Title}}</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<section class="card">
<p class="status {{if .Active}}ok{{else}}bad{{end}}">{{.Status}}</p>
{{- if .Hint}}
<p class="hint">{{.Hint}}</p>
{{- end}}
<p class="label">Трафик</p>
<p class="value">{{.Traffic}}</p>
{{- if .Limited}}
<progress value="{{.Percent}}" max="100"></progress>
{{- end}}
{{- if .PaidUntil}}
<p class="label">Оплачено до</p>
<p class="value">{{.PaidUntil}}</p>
{{- end}}
</section>
{{- if .Link}}
<h2>Подключить</h2>
{{- range .Apps}}
<section class="card">
<a class="btn" href="{{.Deep}}">Добавить в {{.Name}}</a>
<p class="stores">Нет приложения?{{range $i, $s := .Stores}}{{if $i}} ·{{end}} <a href="{{$s.URL}}">{{$s.Name}}</a>{{end}}</p>
</section>
{{- end}}
<section class="card">
<p class="hint">Другой клиент — вставь ссылку вручную:</p>
<input id="link" readonly value="{{.Link}}">
<button class="btn" id="copy" type="button">Скопировать ссылку</button>
</section>
{{- end}}
{{- if .Support}}
<p class="support"><a href="{{.Support}}">Поддержка</a></p>
{{- end}}
</main>
<script>{{.Script}}</script>
</body>
</html>
`

var (
	pageTemplate = template.Must(template.New("page").Parse(pageHTML))
	pageCSP      = fmt.Sprintf("default-src 'none'; style-src %s; script-src %s; img-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
		cspHash(pageStyle), cspHash(pageScript))
	hostPattern = regexp.MustCompile(`^(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9.-]+)(:[0-9]{1,5})?$`)
	ruMonths    = [...]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
)

type store struct {
	Name string
	URL  string
}

type app struct {
	Name   string
	Scheme string
	Stores []store
}

var pageApps = []app{
	{Name: "Happ", Scheme: "happ://add/", Stores: []store{
		{"App Store (RU)", "https://apps.apple.com/ru/app/happ-proxy-utility-plus/id6746188973"},
		{"App Store", "https://apps.apple.com/us/app/happ-proxy-utility/id6504287215"},
		{"Google Play", "https://play.google.com/store/apps/details?id=com.happproxy"},
	}},
	{Name: "v2RayTun", Scheme: "v2raytun://import/", Stores: []store{
		{"App Store", "https://apps.apple.com/app/id6476628951"},
		{"Google Play", "https://play.google.com/store/apps/details?id=com.v2raytun.android"},
	}},
}

type pageApp struct {
	Name   string
	Deep   template.URL
	Stores []store
}

type pageData struct {
	Title     string
	Style     template.CSS
	Script    template.JS
	Active    bool
	Status    string
	Hint      string
	Traffic   string
	Limited   bool
	Percent   int64
	PaidUntil string
	Link      string
	Apps      []pageApp
	Support   string
}

func cspHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func wantsPage(r *http.Request) bool {
	return r.URL.Query().Get("format") == "" &&
		strings.HasPrefix(r.UserAgent(), "Mozilla/") &&
		strings.Contains(r.Header.Get("Accept"), "text/html")
}

func subscriptionLink(r *http.Request, token string) string {
	if !hostPattern.MatchString(r.Host) {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	switch r.Header.Get("X-Forwarded-Proto") {
	case "https":
		scheme = "https"
	case "http":
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/" + token
}

func (s *Server) servePage(ctx context.Context, w http.ResponseWriter, r *http.Request, token string) outcome {
	res := outcome{format: FormatHTML}
	ctx, cancel := context.WithTimeout(ctx, coreTimeout)
	defer cancel()
	view, err := s.core.Subscription(ctx, Request{SubToken: token, UserAgent: r.UserAgent()})
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		res.code, res.status = http.StatusNotFound, "not_found"
		return res
	case err != nil:
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		res.code, res.status, res.err = http.StatusServiceUnavailable, "core_unavailable", err
		return res
	}
	res.status = view.Status
	data := s.pageData(view, subscriptionLink(r, token))
	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, data); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		res.code, res.status, res.err = http.StatusInternalServerError, "render_failed", err
		return res
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pageCSP)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
	res.code = http.StatusOK
	return res
}

func (s *Server) pageData(view *View, link string) pageData {
	d := pageData{Title: s.settings.Title, Style: template.CSS(pageStyle), Script: template.JS(pageScript), Support: s.settings.SupportURL, Link: link}
	switch view.Status {
	case StatusOK:
		d.Active, d.Status = true, "✅ Подписка активна"
	case StatusHWIDRequired:
		d.Active, d.Status = true, "✅ Подписка активна"
		d.Hint = "Подключение — только в Happ или v2RayTun: тариф проверяет устройство."
	case StatusExpired:
		d.Status = StubExpired
	case StatusTrafficExceeded:
		d.Status = StubTrafficExceeded
	default:
		d.Status = StubDenied
	}
	if view.TrafficLimitBytes != nil && *view.TrafficLimitBytes > 0 {
		limit := *view.TrafficLimitBytes
		d.Limited = true
		d.Traffic = formatBytes(view.TrafficUsedBytes) + " из " + formatBytes(limit)
		d.Percent = min(view.TrafficUsedBytes, limit) * 100 / limit
	} else {
		d.Traffic = formatBytes(view.TrafficUsedBytes) + " · без лимита"
	}
	if view.ExpiresAt != nil {
		d.PaidUntil = ruDate(view.ExpiresAt.In(s.loc))
	}
	if link != "" {
		for _, a := range pageApps {
			d.Apps = append(d.Apps, pageApp{Name: a.Name, Deep: template.URL(a.Scheme + link), Stores: a.Stores})
		}
	}
	return d
}

func ruDate(t time.Time) string {
	zone, _ := t.Zone()
	return fmt.Sprintf("%d %s %d, %02d:%02d %s", t.Day(), ruMonths[t.Month()-1], t.Year(), t.Hour(), t.Minute(), zone)
}

func formatBytes(n int64) string {
	const (
		mib = 1 << 20
		gib = 1 << 30
		tib = 1 << 40
	)
	switch {
	case n >= tib:
		return decimal(float64(n)/tib) + " ТБ"
	case n >= gib:
		return decimal(float64(n)/gib) + " ГБ"
	default:
		return strconv.FormatInt(n/mib, 10) + " МБ"
	}
}

func decimal(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return strings.Replace(s, ".", ",", 1)
}
