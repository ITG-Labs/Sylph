package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/yookassa"
)

const (
	CategoryNode    = "node"
	CategoryService = "service"
	CategoryMoney   = "money"
	CategoryCert    = "cert"

	nodeDelaySec       = 120
	serviceSilentAfter = 3 * time.Minute
	cronExpireStale    = 5 * time.Minute
	yookassaDownAfter  = 5 * time.Minute
	pendingStuckAfter  = time.Hour
	conflictRecent     = 2 * time.Minute
	certSoon           = 30 * 24 * time.Hour
	certUrgent         = 7 * 24 * time.Hour
	problemTextRunes   = 300
)

var categoryOrder = []string{CategoryNode, CategoryService, CategoryMoney, CategoryCert}

type Problem struct {
	Key      string     `json:"key"`
	Category string     `json:"category"`
	Level    int        `json:"level"`
	Text     string     `json:"text"`
	Since    *time.Time `json:"since,omitempty"`
	DelaySec int        `json:"delay_sec"`
}

type ServiceView struct {
	model.ServiceInfo
	Alive bool `json:"alive"`
}

type CoreInfo struct {
	Version      string     `json:"version"`
	CertNotAfter *time.Time `json:"cert_not_after"`
	CANotAfter   *time.Time `json:"ca_not_after"`
}

type MonitorState struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Problems    []Problem      `json:"problems"`
	Nodes       map[string]int `json:"nodes"`
	Services    []ServiceView  `json:"services"`
	Core        CoreInfo       `json:"core"`
}

func nodeKey(id int64, kind string) string {
	return fmt.Sprintf("node:%d:%s", id, kind)
}

func (s *Service) SetCertExpiry(server, ca time.Time) {
	s.coreCertNotAfter, s.caNotAfter = &server, &ca
}

func (s *Service) ServiceHeartbeat(ctx context.Context, hb model.ServiceHeartbeat) error {
	if err := validText("version", hb.Version, 64); err != nil {
		return err
	}
	if hb.UptimeSec < 0 {
		return fieldErr("uptime_sec", "must be non-negative")
	}
	return s.store.UpsertServiceHeartbeat(ctx, hb)
}

func (s *Service) ForgetService(ctx context.Context, cn string) error {
	return s.store.DeleteService(ctx, cn)
}

func (s *Service) MarkCronExpire(ctx context.Context) error {
	return s.store.MarkHealthOK(ctx, model.HealthCronExpire)
}

func (s *Service) coreInfo() CoreInfo {
	return CoreInfo{Version: version.Version, CertNotAfter: s.coreCertNotAfter, CANotAfter: s.caNotAfter}
}

func (s *Service) serviceViews(ctx context.Context, now time.Time) ([]ServiceView, error) {
	list, err := s.store.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ServiceView, 0, len(list))
	for _, si := range list {
		out = append(out, ServiceView{ServiceInfo: si, Alive: now.Sub(si.LastSeen) <= serviceSilentAfter})
	}
	return out, nil
}

func (s *Service) MonitorState(ctx context.Context) (*MonitorState, error) {
	now, err := s.store.DBNow(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	services, err := s.serviceViews(ctx, now)
	if err != nil {
		return nil, err
	}
	marks, err := s.store.HealthMarks(ctx)
	if err != nil {
		return nil, err
	}
	attention, err := s.store.AttentionCounts(ctx, now.Add(-pendingStuckAfter))
	if err != nil {
		return nil, err
	}
	st := &MonitorState{
		GeneratedAt: now,
		Problems:    []Problem{},
		Nodes:       map[string]int{NodeOnline: 0, NodeDegraded: 0, NodeOffline: 0, NodeNever: 0, NodeDisabled: 0},
		Services:    services,
		Core:        s.coreInfo(),
	}
	add := func(p Problem) {
		p.Text = clip(p.Text)
		st.Problems = append(st.Problems, p)
	}
	for i := range nodes {
		n := &nodes[i]
		st.Nodes[n.Status]++
		for _, p := range s.nodeProblems(n, now) {
			add(p)
		}
	}
	for _, sv := range services {
		name := serviceName(sv.Role, sv.CertCN)
		if !sv.Alive {
			since := sv.LastSeen
			add(Problem{Key: "service:" + sv.CertCN + ":silent", Category: CategoryService, Level: 2, Text: name + " молчит: нет пульса", Since: &since})
		} else if sv.TelegramConflictAt != nil && now.Sub(*sv.TelegramConflictAt) <= conflictRecent {
			add(Problem{Key: "service:" + sv.CertCN + ":conflict", Category: CategoryService, Level: 2, Text: name + ": второй экземпляр с тем же токеном отнимает апдейты Telegram", Since: sv.TelegramConflictAt})
		}
		if sv.CertNotAfter != nil {
			if p, ok := certProblem("cert:"+sv.CertCN, "Серт "+name, *sv.CertNotAfter, now); ok {
				add(p)
			}
		}
	}
	if m, ok := marks[model.HealthCronExpire]; ok && (m.OKAt == nil || now.Sub(*m.OKAt) > cronExpireStale) {
		add(Problem{Key: "cron:expire", Category: CategoryService, Level: 2, Text: "Крон не закрывает истёкшие периоды", Since: m.OKAt})
	}
	if m, ok := marks[model.HealthYooKassa]; ok && m.FailingSince != nil && now.Sub(*m.FailingSince) >= yookassaDownAfter {
		add(Problem{Key: "money:yookassa", Category: CategoryMoney, Level: 2, Text: "ЮKassa не отвечает ядру: " + m.Error, Since: m.FailingSince})
	}
	if attention.Mismatch > 0 {
		add(Problem{Key: "money:mismatch", Category: CategoryMoney, Level: 2, Text: fmt.Sprintf("Платежи с несовпавшей суммой (mismatch): %d", attention.Mismatch)})
	}
	if attention.StuckPending > 0 {
		add(Problem{Key: "money:pending", Category: CategoryMoney, Level: 1, Text: fmt.Sprintf("Платежи висят в pending дольше часа: %d", attention.StuckPending)})
	}
	if attention.PaidWhileBanned > 0 {
		add(Problem{Key: "money:banned", Category: CategoryMoney, Level: 1, Text: fmt.Sprintf("Оплаты от забаненных (проведены после бана, не возвращены): %d", attention.PaidWhileBanned)})
	}
	if s.coreCertNotAfter != nil {
		if p, ok := certProblem("cert:core", "Серт ядра", *s.coreCertNotAfter, now); ok {
			add(p)
		}
	}
	if s.caNotAfter != nil {
		if p, ok := certProblem("cert:ca", "Корневой серт CA", *s.caNotAfter, now); ok {
			add(p)
		}
	}
	sort.SliceStable(st.Problems, func(i, j int) bool {
		a, b := st.Problems[i], st.Problems[j]
		if a.Category != b.Category {
			return slices.Index(categoryOrder, a.Category) < slices.Index(categoryOrder, b.Category)
		}
		return a.Key < b.Key
	})
	return st, nil
}

func (s *Service) nodeProblems(n *model.Node, now time.Time) []Problem {
	if n.Status == NodeDisabled || n.Status == NodeNever {
		return nil
	}
	name := "Нода «" + n.Name + "»"
	var out []Problem
	if n.Status == NodeOffline {
		out = append(out, Problem{Key: nodeKey(n.ID, "offline"), Category: CategoryNode, Level: 2, Text: name + " лежит: нет heartbeat", Since: n.LastHeartbeat, DelaySec: nodeDelaySec})
	} else {
		if n.LastSyncAt == nil || now.Sub(*n.LastSyncAt) > s.nodeStale {
			out = append(out, Problem{Key: nodeKey(n.ID, "sync"), Category: CategoryNode, Level: 2, Text: name + " не синкается с ядром", Since: n.LastSyncAt, DelaySec: nodeDelaySec})
		}
		if n.ConfigError != nil {
			out = append(out, Problem{Key: nodeKey(n.ID, "config"), Category: CategoryNode, Level: 2, Text: name + ": ошибка конфига — " + *n.ConfigError, DelaySec: nodeDelaySec})
		}
		if failed := failedGeo(n.GeoStatus); len(failed) > 0 {
			out = append(out, Problem{Key: nodeKey(n.ID, "geo"), Category: CategoryNode, Level: 1, Text: name + ": не качается geo-база " + strings.Join(failed, ", "), DelaySec: nodeDelaySec})
		}
		if n.RoutingError != nil {
			out = append(out, Problem{Key: nodeKey(n.ID, "routing"), Category: CategoryNode, Level: 1, Text: name + ": правила по тарифу не встали — " + *n.RoutingError, DelaySec: nodeDelaySec})
		}
	}
	if n.CertNotAfter != nil {
		if p, ok := certProblem("cert:node:"+n.CertCN, "Серт ноды «"+n.Name+"» ("+n.CertCN+")", *n.CertNotAfter, now); ok {
			out = append(out, p)
		}
	}
	return out
}

func failedGeo(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var status map[string]struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &status) != nil {
		return nil
	}
	var out []string
	for name, st := range status {
		if st.Error != "" {
			out = append(out, name+" ("+st.Error+")")
		}
	}
	sort.Strings(out)
	return out
}

func certProblem(key, what string, notAfter, now time.Time) (Problem, bool) {
	left := notAfter.Sub(now)
	date := notAfter.UTC().Format(time.DateOnly)
	p := Problem{Key: key, Category: CategoryCert}
	switch {
	case left <= 0:
		p.Level, p.Text = 3, what+" истёк "+date
	case left <= certUrgent:
		p.Level, p.Text = 2, fmt.Sprintf("%s истекает %s (через %d дн.)", what, date, int(left.Hours()/24))
	case left <= certSoon:
		p.Level, p.Text = 1, fmt.Sprintf("%s истекает %s (через %d дн.)", what, date, int(left.Hours()/24))
	default:
		return Problem{}, false
	}
	return p, true
}

func serviceName(role, cn string) string {
	names := map[string]string{"bot": "Бот", "subs": "subs", "cron": "Крон", "panel": "Панель", "monitor": "Монитор"}
	name := names[role]
	if name == "" {
		name = role
	}
	if cn != role {
		name += " (" + cn + ")"
	}
	return name
}

func clip(s string) string {
	r := []rune(s)
	if len(r) > problemTextRunes {
		return string(r[:problemTextRunes]) + "…"
	}
	return s
}

type trackedProvider struct {
	PaymentProvider
	svc *Service
}

func (t trackedProvider) CreatePayment(ctx context.Context, req yookassa.CreateRequest, key string) (*yookassa.Payment, error) {
	p, err := t.PaymentProvider.CreatePayment(ctx, req, key)
	t.svc.markYooKassa(err)
	return p, err
}

func (t trackedProvider) GetPayment(ctx context.Context, id string) (*yookassa.Payment, error) {
	p, err := t.PaymentProvider.GetPayment(ctx, id)
	t.svc.markYooKassa(err)
	return p, err
}

func yookassaDown(err error) bool {
	var apiErr *yookassa.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status >= 500 || apiErr.Status == 401 || apiErr.Status == 403
	}
	return !errors.Is(err, yookassa.ErrNotFound)
}

func (s *Service) markYooKassa(err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var markErr error
	if err != nil && yookassaDown(err) {
		markErr = s.store.MarkHealthFailed(ctx, model.HealthYooKassa, clip(err.Error()))
	} else {
		markErr = s.store.MarkHealthOK(ctx, model.HealthYooKassa)
	}
	if markErr != nil {
		s.log.Warn("health_mark_failed", "name", model.HealthYooKassa, "error", markErr.Error())
	}
}
