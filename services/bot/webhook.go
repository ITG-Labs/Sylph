package bot

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const webhookMaxBody = 64 << 10

type Notifier func(ctx context.Context, telegramID int64, paidUntil *time.Time) error

type Webhook struct {
	core    Core
	notify  Notifier
	allow   []netip.Prefix
	trusted []netip.Prefix
	log     *slog.Logger
}

func NewWebhook(core Core, notify Notifier, allow, trusted []netip.Prefix, log *slog.Logger) *Webhook {
	return &Webhook{core: core, notify: notify, allow: allow, trusted: trusted, log: log}
}

func (h *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	ip, ok := h.clientIP(r)
	if !ok || !inAny(h.allow, ip) {
		h.log.LogAttrs(ctx, slog.LevelWarn, "webhook_rejected", slog.String("remote", r.RemoteAddr), slog.String("ip", ip.String()))
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var body struct {
		Event  string `json:"event"`
		Object struct {
			ID string `json:"id"`
		} `json:"object"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, webhookMaxBody))
	err := dec.Decode(&body)
	if err == nil && dec.More() {
		err = errors.New("unexpected data after the JSON object")
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id := body.Object.ID
	if !validPaymentID(id) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	h.log.LogAttrs(ctx, slog.LevelInfo, "webhook_received", slog.String("event", body.Event), slog.String("payment_id", id), slog.String("ip", ip.String()))
	if !strings.HasPrefix(body.Event, "payment.") {
		w.WriteHeader(http.StatusOK)
		return
	}
	chk, err := h.core.CheckPayment(ctx, id)
	if errors.Is(err, ErrNotFound) {
		h.log.LogAttrs(ctx, slog.LevelWarn, "webhook_unknown_payment", slog.String("payment_id", id))
		w.WriteHeader(http.StatusOK)
		return
	}
	if err != nil {
		h.log.LogAttrs(ctx, slog.LevelError, "webhook_check_failed", slog.String("payment_id", id), slog.String("error", err.Error()))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if chk.Applied && chk.TelegramID != nil {
		if err := h.notify(ctx, *chk.TelegramID, chk.PaidUntil); err != nil {
			h.log.LogAttrs(ctx, slog.LevelError, "payment_notify_failed", slog.String("payment_id", id), slog.String("error", err.Error()))
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Webhook) clientIP(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	peer = peer.Unmap()
	if !inAny(h.trusted, peer) {
		return peer, true
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if hop == "" {
			continue
		}
		addr, err := netip.ParseAddr(hop)
		if err != nil {
			return netip.Addr{}, false
		}
		addr = addr.Unmap()
		if !inAny(h.trusted, addr) {
			return addr, true
		}
	}
	return peer, true
}

func validPaymentID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}
