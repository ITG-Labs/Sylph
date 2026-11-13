package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/ITG-Labs/sylph/pkg/clientroute"
	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxGeoTags = 20000

var geoTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,79}$`)

type GeoTagsStatus struct {
	Loaded    bool       `json:"loaded"`
	Release   string     `json:"release"`
	FetchedAt *time.Time `json:"fetched_at"`
}

type ClientRoutingView struct {
	Rules     []clientroute.Rule    `json:"rules"`
	Revision  int64                 `json:"revision"`
	UpdatedAt *time.Time            `json:"updated_at"`
	Geo       GeoTagsStatus         `json:"geo"`
	Issues    [][]clientroute.Issue `json:"issues"`
	Missing   [][]string            `json:"missing"`
	Overlaps  []clientroute.Overlap `json:"overlaps"`
}

type ClientRoutingFeed struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Rules     []clientroute.Rule `json:"rules"`
}

func (s *Service) ClientGeoTags(ctx context.Context) (*clientroute.Tags, error) {
	raw, err := s.store.GetClientGeoTags(ctx)
	if err != nil || raw == nil {
		return nil, err
	}
	var t clientroute.Tags
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func cleanTags(in []string) []string {
	out := []string{}
	for _, v := range in {
		if geoTagPattern.MatchString(v) && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

func (s *Service) PutClientGeoTags(ctx context.Context, t clientroute.Tags) (bool, error) {
	if len(t.Geosite) > maxGeoTags || len(t.Geoip) > maxGeoTags {
		return false, fieldErr("tags", fmt.Sprintf("at most %d tags per list", maxGeoTags))
	}
	next := clientroute.Tags{Release: t.Release, Geosite: cleanTags(t.Geosite), Geoip: cleanTags(t.Geoip), FetchedAt: time.Now().UTC()}
	if len(next.Geosite) == 0 || len(next.Geoip) == 0 {
		return false, fieldErr("tags", "geosite and geoip lists must not be empty")
	}
	prev, err := s.ClientGeoTags(ctx)
	if err != nil {
		return false, err
	}
	changed := prev == nil || prev.Release != next.Release || !slices.Equal(prev.Geosite, next.Geosite) || !slices.Equal(prev.Geoip, next.Geoip)
	raw, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	return changed, s.store.PutClientGeoTags(ctx, raw)
}

func (s *Service) loadClientRouting(ctx context.Context) (store.ClientRouting, []clientroute.Rule, error) {
	cr, err := s.store.GetClientRouting(ctx)
	if err != nil {
		return cr, nil, err
	}
	rules := []clientroute.Rule{}
	if err := json.Unmarshal(cr.Rules, &rules); err != nil {
		return cr, nil, err
	}
	return cr, rules, nil
}

func (s *Service) GetClientRouting(ctx context.Context) (*ClientRoutingView, error) {
	cr, rules, err := s.loadClientRouting(ctx)
	if err != nil {
		return nil, err
	}
	tags, err := s.ClientGeoTags(ctx)
	if err != nil {
		return nil, err
	}
	v := &ClientRoutingView{
		Rules: rules, Revision: cr.Revision, UpdatedAt: cr.UpdatedAt,
		Issues: make([][]clientroute.Issue, len(rules)), Missing: make([][]string, len(rules)),
		Overlaps: clientroute.Overlaps(rules),
	}
	if v.Overlaps == nil {
		v.Overlaps = []clientroute.Overlap{}
	}
	if tags.Loaded() {
		v.Geo = GeoTagsStatus{Loaded: true, Release: tags.Release, FetchedAt: &tags.FetchedAt}
	}
	for i, r := range rules {
		v.Issues[i] = clientroute.Issues(r)
		if v.Issues[i] == nil {
			v.Issues[i] = []clientroute.Issue{}
		}
		v.Missing[i] = tags.Missing(r)
		if v.Missing[i] == nil {
			v.Missing[i] = []string{}
		}
	}
	return v, nil
}

func (s *Service) PutClientRouting(ctx context.Context, rules []clientroute.Rule, revision int64) (*ClientRoutingView, error) {
	tags, err := s.ClientGeoTags(ctx)
	if err != nil {
		return nil, err
	}
	norm, err := clientroute.Validate(rules, tags)
	var fe *noderoute.FieldError
	if errors.As(err, &fe) {
		return nil, fieldErr(fe.Field, fe.Message)
	}
	if err != nil {
		return nil, err
	}
	for i, r := range norm {
		if r.Action != clientroute.ActionNode {
			continue
		}
		if _, err := s.store.GetNode(ctx, r.NodeID); errors.Is(err, store.ErrNotFound) {
			return nil, fieldErr(fmt.Sprintf("rules[%d].node_id", i), "node not found")
		} else if err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(norm)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.PutClientRouting(ctx, raw, revision); errors.Is(err, store.ErrStaleRevision) {
		return nil, &ConflictError{Field: "revision", Message: "client routing was changed by someone else: reload it"}
	} else if err != nil {
		return nil, err
	}
	return s.GetClientRouting(ctx)
}

func (s *Service) clientRoutingFeed(ctx context.Context) (*ClientRoutingFeed, error) {
	cr, rules, err := s.loadClientRouting(ctx)
	if err != nil || len(rules) == 0 || cr.UpdatedAt == nil {
		return nil, err
	}
	tags, err := s.ClientGeoTags(ctx)
	if err != nil {
		return nil, err
	}
	feed := &ClientRoutingFeed{UpdatedAt: *cr.UpdatedAt, Rules: []clientroute.Rule{}}
	for _, r := range rules {
		if len(tags.Missing(r)) == 0 {
			feed.Rules = append(feed.Rules, r)
		}
	}
	if len(feed.Rules) == 0 {
		return nil, nil
	}
	return feed, nil
}
