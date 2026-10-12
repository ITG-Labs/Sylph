package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	StatusOK              = "ok"
	StatusExpired         = "expired"
	StatusTrafficExceeded = "traffic_exceeded"
	StatusHWIDRequired    = "hwid_required"
	StatusDeviceLimit     = "device_limit"
)

type SubscriptionRequest struct {
	SubToken    string
	HWID        string
	UserAgent   string
	DeviceOS    string
	DeviceModel string
}

type Connection struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	UUID        string `json:"uuid"`
	Flow        string `json:"flow"`
	Transport   string `json:"transport"`
	Security    string `json:"security"`
	SNI         string `json:"sni"`
	PublicKey   string `json:"public_key"`
	ShortID     string `json:"short_id"`
	Fingerprint string `json:"fingerprint"`
}

type SubscriptionView struct {
	Status            string       `json:"status"`
	TrafficUsedBytes  int64        `json:"traffic_used_bytes"`
	TrafficLimitBytes *int64       `json:"traffic_limit_bytes"`
	ExpiresAt         *time.Time   `json:"expires_at"`
	Connections       []Connection `json:"connections"`
}

func (s *Service) Subscription(ctx context.Context, req SubscriptionRequest) (*SubscriptionView, error) {
	u, err := s.store.GetUserBySubToken(ctx, req.SubToken)
	if err != nil {
		return nil, err
	}
	view, err := s.subscriptionView(ctx, u, req)
	if err != nil {
		return nil, err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "subscription_view",
		slog.Int64("user_id", u.ID),
		slog.String("status", view.Status),
		slog.String("client", clientFamily(req.UserAgent)),
		slog.Int("connections", len(view.Connections)))
	return view, nil
}

func (s *Service) subscriptionView(ctx context.Context, u *model.User, req SubscriptionRequest) (*SubscriptionView, error) {
	view := &SubscriptionView{Connections: []Connection{}}
	sub, err := s.store.GetActiveSubscription(ctx, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		view.Status = StatusExpired
		return view, nil
	}
	if err != nil {
		return nil, err
	}
	plan, err := s.store.GetPlan(ctx, sub.PlanID)
	if err != nil {
		return nil, err
	}
	view.ExpiresAt = sub.PaidUntil
	view.TrafficUsedBytes = sub.TrafficUsedBytes
	view.TrafficLimitBytes = plan.TrafficLimitBytes
	if plan.TrafficLimitBytes != nil && sub.TrafficUsedBytes >= *plan.TrafficLimitBytes {
		view.Status = StatusTrafficExceeded
		return view, nil
	}
	if plan.DeviceLimit != nil && req.HWID == "" {
		view.Status = StatusHWIDRequired
		return view, nil
	}
	if req.HWID != "" {
		allowed, err := s.store.RegisterDevice(ctx, u.ID, store.DeviceInfo{HWID: req.HWID, UserAgent: req.UserAgent, OS: req.DeviceOS, Model: req.DeviceModel}, plan.DeviceLimit)
		if err != nil {
			return nil, err
		}
		if !allowed {
			view.Status = StatusDeviceLimit
			return view, nil
		}
	}
	view.Status = StatusOK
	keys, err := s.store.GetUserKeys(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return view, nil
	}
	conns, err := s.connections(ctx, plan.LocationGroupID, keys[0].UUID)
	if err != nil {
		return nil, err
	}
	view.Connections = conns
	return view, nil
}

func (s *Service) connections(ctx context.Context, groupID *int64, uuid string) ([]Connection, error) {
	endpoints, err := s.store.ListVisibleEndpoints(ctx, groupID)
	if err != nil {
		return nil, err
	}
	perNode := map[int64]int{}
	for _, e := range endpoints {
		perNode[e.Inbound.NodeID]++
	}
	out := make([]Connection, 0, len(endpoints))
	for _, e := range endpoints {
		spec, err := SpecOf(&e.Inbound)
		if err != nil {
			return nil, err
		}
		c := Connection{
			Name: e.NodeName, Address: e.NodeAddress, Port: spec.Port, UUID: uuid,
			Flow: inbound.Flow(spec.Transport, spec.Security), Transport: spec.Transport, Security: spec.Security,
		}
		if perNode[e.Inbound.NodeID] > 1 {
			label := spec.Label
			if label == "" {
				label = spec.Tag
			}
			c.Name = e.NodeName + " · " + label
		}
		if r := spec.Reality; r != nil {
			pub, err := inbound.PublicKey(r.PrivateKey)
			if err != nil {
				return nil, err
			}
			c.PublicKey, c.Fingerprint = pub, r.Fingerprint
			if len(r.ServerNames) > 0 {
				c.SNI = r.ServerNames[0]
			}
			if len(r.ShortIDs) > 0 {
				c.ShortID = r.ShortIDs[0]
			}
		}
		out = append(out, c)
	}
	uniqueNames(out)
	return out, nil
}

func uniqueNames(conns []Connection) {
	taken := make(map[string]bool, len(conns))
	for i := range conns {
		name := conns[i].Name
		for n := 2; taken[name]; n++ {
			name = fmt.Sprintf("%s %d", conns[i].Name, n)
		}
		taken[name] = true
		conns[i].Name = name
	}
}

func clientFamily(ua string) string {
	family := ua
	if i := strings.IndexAny(family, "/ ;("); i >= 0 {
		family = family[:i]
	}
	if len(family) > 32 {
		family = family[:32]
	}
	if family == "" {
		return "unknown"
	}
	return family
}
