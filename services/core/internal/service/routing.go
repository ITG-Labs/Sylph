package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/pkg/netaddr"
	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxGeoURLBytes = 1024

type ProfileWarning struct {
	Outbound string `json:"outbound"`
	NodeID   int64  `json:"node_id"`
	Problem  string `json:"problem"`
}

type ProfileView struct {
	ID        int64            `json:"id"`
	Name      string           `json:"name"`
	Doc       json.RawMessage  `json:"doc"`
	Revision  int64            `json:"revision"`
	Nodes     int              `json:"nodes"`
	NodeIDs   []int64          `json:"node_ids"`
	Warnings  []ProfileWarning `json:"warnings"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type GeoSourceInput struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	SHA256URL string `json:"sha256_url"`
}

func parseProfile(raw json.RawMessage) (*noderoute.Profile, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p noderoute.Profile
	if err := dec.Decode(&p); err != nil {
		return nil, fieldErr("doc", "must be a routing profile: "+err.Error())
	}
	return &p, nil
}

func docField(err error) error {
	var fe *noderoute.FieldError
	if errors.As(err, &fe) {
		return fieldErr("doc."+fe.Field, fe.Message)
	}
	return err
}

func mergeSecrets(p, prev *noderoute.Profile) error {
	for i := range p.Outbounds {
		o := &p.Outbounds[i]
		if o.Kind == noderoute.KindRaw && noderoute.RawHasMasked(o.Raw) {
			j := slices.IndexFunc(prev.Outbounds, func(old noderoute.Outbound) bool {
				return old.Tag == o.Tag && old.Kind == noderoute.KindRaw && noderoute.SameJSON(noderoute.MaskRaw(old.Raw), o.Raw)
			})
			if j < 0 {
				return fieldErr(fmt.Sprintf("doc.outbounds[%d].raw", i), "secrets are hidden as ***: enter them again or leave the outbound unchanged")
			}
			o.Raw = prev.Outbounds[j].Raw
			continue
		}
		if o.Kind != noderoute.KindProxy || o.Proxy == nil {
			continue
		}
		noderoute.NormalizeProxy(o.Proxy)
		o.Proxy.HasSecret = false
		j := slices.IndexFunc(prev.Outbounds, func(old noderoute.Outbound) bool {
			return old.Tag == o.Tag && old.Kind == noderoute.KindProxy && old.Proxy != nil && sameDestination(old.Proxy, o.Proxy)
		})
		if j < 0 {
			continue
		}
		if o.Proxy.UUID == "" {
			o.Proxy.UUID = prev.Outbounds[j].Proxy.UUID
		}
		if o.Proxy.Password == "" {
			o.Proxy.Password = prev.Outbounds[j].Proxy.Password
		}
	}
	return nil
}

func sameDestination(a, b *noderoute.Proxy) bool {
	return a.Protocol == b.Protocol && strings.EqualFold(a.Address, b.Address) && a.Port == b.Port && a.Username == b.Username &&
		strings.EqualFold(a.Security, b.Security) && strings.EqualFold(a.SNI, b.SNI) && a.Method == b.Method
}

func maskSecrets(p *noderoute.Profile) {
	for i := range p.Outbounds {
		if p.Outbounds[i].Kind == noderoute.KindRaw {
			p.Outbounds[i].Raw = noderoute.MaskRaw(p.Outbounds[i].Raw)
		}
		x := p.Outbounds[i].Proxy
		if x == nil {
			continue
		}
		if x.UUID != "" || x.Password != "" {
			x.HasSecret = true
		}
		x.UUID, x.Password = "", ""
	}
}

func (s *Service) nodesWithProfile(ctx context.Context, profileID int64) ([]int64, error) {
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	out := []int64{}
	for _, n := range nodes {
		if n.ProfileID != nil && *n.ProfileID == profileID {
			out = append(out, n.ID)
		}
	}
	return out, nil
}

func (s *Service) checkProfile(ctx context.Context, p *noderoute.Profile, profileID int64) error {
	if err := noderoute.Validate(p); err != nil {
		return docField(err)
	}
	for i, r := range p.Rules {
		for _, id := range r.Plans {
			if _, err := s.store.GetPlan(ctx, id); errors.Is(err, store.ErrNotFound) {
				return fieldErr(fmt.Sprintf("doc.rules[%d].plans", i), fmt.Sprintf("plan %d not found", id))
			} else if err != nil {
				return err
			}
		}
	}
	holders := []int64{}
	if profileID != 0 {
		var err error
		if holders, err = s.nodesWithProfile(ctx, profileID); err != nil {
			return err
		}
	}
	for i, o := range p.Outbounds {
		if o.Kind != noderoute.KindCascade {
			continue
		}
		field := fmt.Sprintf("doc.outbounds[%d].cascade.node_id", i)
		if _, err := s.store.GetNode(ctx, o.Cascade.NodeID); errors.Is(err, store.ErrNotFound) {
			return fieldErr(field, "node not found")
		} else if err != nil {
			return err
		}
		if slices.Contains(holders, o.Cascade.NodeID) {
			return fieldErr(field, "this node uses the profile itself: it cannot cascade to itself")
		}
	}
	sources, err := s.store.ListGeoSources(ctx)
	if err != nil {
		return err
	}
	kinds := map[string]string{}
	for _, g := range sources {
		kinds[g.Name] = g.Kind
	}
	for i, r := range p.Rules {
		single := noderoute.Profile{Rules: []noderoute.Rule{r}}
		domain, ip := single.Sources()
		for _, name := range domain {
			if kinds[name] != "domain" {
				return fieldErr(fmt.Sprintf("doc.rules[%d].domain", i), fmt.Sprintf("%q is not a geo source of domains", name))
			}
		}
		for _, name := range ip {
			if kinds[name] != "ip" {
				return fieldErr(fmt.Sprintf("doc.rules[%d].ip", i), fmt.Sprintf("%q is not a geo source of IPs", name))
			}
		}
	}
	return nil
}

func (s *Service) profileView(ctx context.Context, row *store.ProfileRow) (*ProfileView, error) {
	p, err := parseProfile(row.Doc)
	if err != nil {
		return nil, err
	}
	nodes, err := s.nodesWithProfile(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	var from int64
	if len(nodes) > 0 {
		from = nodes[0]
	}
	_, warnings, err := s.resolveProfile(ctx, from, p)
	if err != nil {
		return nil, err
	}
	maskSecrets(p)
	doc, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return &ProfileView{ID: row.ID, Name: row.Name, Doc: doc, Revision: row.Revision, Nodes: row.Nodes, NodeIDs: nodes,
		Warnings: warnings, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

func (s *Service) CreateProfile(ctx context.Context, name string, doc json.RawMessage) (*ProfileView, error) {
	if err := validName("name", name); err != nil {
		return nil, err
	}
	p, err := parseProfile(doc)
	if err != nil {
		return nil, err
	}
	if err := s.checkProfile(ctx, p, 0); err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	row, err := s.store.CreateProfile(ctx, name, normalized)
	if err != nil {
		return nil, conflictOr(err, "name", "profile already exists")
	}
	return s.profileView(ctx, row)
}

func (s *Service) UpdateProfile(ctx context.Context, id int64, name string, doc json.RawMessage, revision int64) (*ProfileView, error) {
	current, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := validName("name", name); err != nil {
		return nil, err
	}
	p, err := parseProfile(doc)
	if err != nil {
		return nil, err
	}
	prev, err := parseProfile(current.Doc)
	if err != nil {
		return nil, err
	}
	if err := mergeSecrets(p, prev); err != nil {
		return nil, err
	}
	if err := s.checkProfile(ctx, p, id); err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	row, err := s.store.UpdateProfile(ctx, id, name, normalized, revision)
	if errors.Is(err, store.ErrStaleRevision) {
		return nil, &ConflictError{Field: "revision", Message: "the profile was changed by someone else: reload it"}
	}
	if err != nil {
		return nil, conflictOr(err, "name", "profile already exists")
	}
	if err := s.store.RefreshLinks(ctx); err != nil {
		return nil, err
	}
	return s.profileView(ctx, row)
}

func (s *Service) GetProfile(ctx context.Context, id int64) (*ProfileView, error) {
	row, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.profileView(ctx, row)
}

func (s *Service) ListProfiles(ctx context.Context) ([]ProfileView, error) {
	rows, err := s.store.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProfileView, 0, len(rows))
	for i := range rows {
		v, err := s.profileView(ctx, &rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

func (s *Service) DeleteProfile(ctx context.Context, id int64) error {
	err := s.store.DeleteProfile(ctx, id)
	if errors.Is(err, store.ErrProfileInUse) {
		return &ConflictError{Field: "profile", Message: "profile is used by nodes: switch them to another profile first"}
	}
	return err
}

func (s *Service) cascadeTarget(ctx context.Context, from, to int64) (*noderoute.Proxy, string, error) {
	n, err := s.store.GetNode(ctx, to)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "missing", nil
	}
	if err != nil {
		return nil, "", err
	}
	switch {
	case !n.Enabled:
		return nil, "disabled", nil
	case n.Address == "":
		return nil, "no_address", nil
	case !netaddr.PublicHost(n.Address):
		return nil, "private_address", nil
	}
	list, err := s.store.ListNodeInbounds(ctx, to)
	if err != nil {
		return nil, "", err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Tag < list[j].Tag })
	for i := range list {
		spec, err := SpecOf(&list[i])
		if err != nil {
			return nil, "", err
		}
		if spec.Security != inbound.SecurityReality || spec.Reality == nil || len(spec.Reality.ServerNames) == 0 {
			continue
		}
		pub, err := inbound.PublicKey(spec.Reality.PrivateKey)
		if err != nil {
			return nil, "", err
		}
		key := "00000000-0000-4000-8000-000000000000"
		if from != 0 {
			if key, err = s.store.LinkKey(ctx, from, to); errors.Is(err, store.ErrNotFound) {
				return nil, "no_link", nil
			} else if err != nil {
				return nil, "", err
			}
		}
		shortID := ""
		if len(spec.Reality.ShortIDs) > 0 {
			shortID = spec.Reality.ShortIDs[0]
		}
		return &noderoute.Proxy{Protocol: "vless", Address: n.Address, Port: spec.Port, UUID: key, Security: "reality",
			Flow: inbound.Flow(spec.Transport, spec.Security), SNI: spec.Reality.ServerNames[0], Fingerprint: spec.Reality.Fingerprint,
			PublicKey: pub, ShortID: shortID}, "", nil
	}
	return nil, "no_inbound", nil
}

func (s *Service) resolveProfile(ctx context.Context, from int64, p *noderoute.Profile) (*noderoute.Profile, []ProfileWarning, error) {
	out := *p
	out.Outbounds = slices.Clone(p.Outbounds)
	warnings := []ProfileWarning{}
	for i, o := range out.Outbounds {
		if o.Kind != noderoute.KindCascade || o.Cascade == nil {
			continue
		}
		proxy, problem, err := s.cascadeTarget(ctx, from, o.Cascade.NodeID)
		if err != nil {
			return nil, nil, err
		}
		if proxy == nil {
			if from != 0 || problem != "no_link" {
				warnings = append(warnings, ProfileWarning{Outbound: o.Tag, NodeID: o.Cascade.NodeID, Problem: problem})
			}
			out.Outbounds[i] = noderoute.Outbound{Tag: o.Tag, Kind: noderoute.KindBlock}
			continue
		}
		out.Outbounds[i] = noderoute.Outbound{Tag: o.Tag, Kind: noderoute.KindProxy, Proxy: proxy}
	}
	return &out, warnings, nil
}

func (s *Service) nodeRouting(ctx context.Context, node *model.Node) (*noderoute.Profile, *SyncGeo, error) {
	if node.ProfileID == nil {
		return nil, nil, nil
	}
	row, err := s.store.GetProfile(ctx, *node.ProfileID)
	if err != nil {
		return nil, nil, err
	}
	p, err := parseProfile(row.Doc)
	if err != nil {
		return nil, nil, err
	}
	resolved, _, err := s.resolveProfile(ctx, node.ID, p)
	if err != nil {
		return nil, nil, err
	}
	if err := noderoute.ValidateResolved(resolved); err != nil {
		return nil, nil, fmt.Errorf("profile %d does not resolve for node %d: %w", row.ID, node.ID, err)
	}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, nil, err
	}
	all, err := s.store.ListGeoSources(ctx)
	if err != nil {
		return nil, nil, err
	}
	domain, ip := resolved.Sources()
	geo := &SyncGeo{Generation: settings.GeoGeneration, Sources: []SyncGeoSource{}}
	for _, g := range all {
		if slices.Contains(domain, g.Name) || slices.Contains(ip, g.Name) {
			geo.Sources = append(geo.Sources, SyncGeoSource{Name: g.Name, Kind: g.Kind, URL: g.URL, SHA256URL: g.SHA256URL})
		}
	}
	return resolved, geo, nil
}

func (s *Service) PreviewProfile(ctx context.Context, id int64, nodeID *int64) (json.RawMessage, error) {
	row, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := parseProfile(row.Doc)
	if err != nil {
		return nil, err
	}
	var from int64
	var serviceKeys []string
	if nodeID != nil {
		if _, err := s.store.GetNode(ctx, *nodeID); err != nil {
			return nil, err
		}
		from = *nodeID
		if serviceKeys, err = s.store.ServiceKeys(ctx, from); err != nil {
			return nil, err
		}
	}
	resolved, _, err := s.resolveProfile(ctx, from, p)
	if err != nil {
		return nil, err
	}
	masked := make([]string, len(serviceKeys))
	for i := range masked {
		masked[i] = "***"
	}
	static, err := noderoute.Render(noderoute.RenderInput{Profile: resolved, ServiceKeys: masked, GeoFile: func(name string) string { return "<" + name + ">" }})
	if err != nil {
		return nil, docField(err)
	}
	for _, o := range static.Outbounds {
		noderoute.MaskValue(o)
	}
	var tail []map[string]any
	for _, r := range noderoute.Tail(resolved, planPlaceholders(resolved), 0) {
		rule := map[string]any{"ruleTag": r.Tag}
		if r.CatchAll {
			rule["network"] = "tcp,udp"
		} else {
			rule["user"] = r.Users
		}
		if r.BalancerTag != "" {
			rule["balancerTag"] = r.BalancerTag
		} else {
			rule["outboundTag"] = r.OutboundTag
		}
		tail = append(tail, rule)
	}
	return json.Marshal(map[string]any{"outbounds": static.Outbounds, "routing": static.Routing, "observatory": static.Observatory, "plan_rules": tail})
}

func planPlaceholders(p *noderoute.Profile) map[int64][]string {
	out := map[int64][]string{}
	for _, id := range p.Plans() {
		out[id] = []string{fmt.Sprintf("<users of plan %d>", id)}
	}
	return out
}

func validSourceURL(field, v string, required bool) error {
	if v == "" {
		if required {
			return fieldErr(field, "is required")
		}
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || !netaddr.PublicHost(u.Hostname()) || len(v) > maxGeoURLBytes {
		return fieldErr(field, "must be a public https URL")
	}
	return validText(field, v, maxGeoURLBytes)
}

func checkGeoSource(in GeoSourceInput) error {
	if !noderoute.SourcePattern.MatchString(in.Name) || noderoute.ReservedSource(in.Name) {
		return fieldErr("name", "must be 1-32 characters of a-z, 0-9 and -, not a built-in prefix")
	}
	if in.Kind != "domain" && in.Kind != "ip" {
		return fieldErr("kind", "must be domain or ip")
	}
	if err := validSourceURL("url", in.URL, true); err != nil {
		return err
	}
	return validSourceURL("sha256_url", in.SHA256URL, false)
}

func (s *Service) profilesUsingSource(ctx context.Context, name string) ([]int64, string, error) {
	rows, err := s.store.ListProfiles(ctx)
	if err != nil {
		return nil, "", err
	}
	var ids []int64
	usage := ""
	for _, row := range rows {
		p, err := parseProfile(row.Doc)
		if err != nil {
			return nil, "", err
		}
		domain, ip := p.Sources()
		switch {
		case slices.Contains(domain, name):
			ids, usage = append(ids, row.ID), "domain"
		case slices.Contains(ip, name):
			ids, usage = append(ids, row.ID), "ip"
		}
	}
	return ids, usage, nil
}

func (s *Service) CreateGeoSource(ctx context.Context, in GeoSourceInput) (*store.GeoSource, error) {
	if err := checkGeoSource(in); err != nil {
		return nil, err
	}
	g, err := s.store.CreateGeoSource(ctx, store.GeoSource{Name: in.Name, Kind: in.Kind, URL: in.URL, SHA256URL: in.SHA256URL})
	return g, conflictOr(err, "name", "geo source already exists")
}

func (s *Service) UpdateGeoSource(ctx context.Context, id int64, in GeoSourceInput) (*store.GeoSource, error) {
	if err := checkGeoSource(in); err != nil {
		return nil, err
	}
	current, err := s.store.GetGeoSource(ctx, id)
	if err != nil {
		return nil, err
	}
	users, _, err := s.profilesUsingSource(ctx, current.Name)
	if err != nil {
		return nil, err
	}
	if len(users) > 0 && in.Name != current.Name {
		return nil, &ConflictError{Field: "name", Message: "routing profiles refer to this name: change them first"}
	}
	if len(users) > 0 && in.Kind != current.Kind {
		return nil, &ConflictError{Field: "kind", Message: "routing profiles use this source as " + current.Kind}
	}
	g, err := s.store.UpdateGeoSource(ctx, store.GeoSource{ID: id, Name: in.Name, Kind: in.Kind, URL: in.URL, SHA256URL: in.SHA256URL})
	if err != nil {
		return nil, conflictOr(err, "name", "geo source already exists")
	}
	if in.URL != current.URL || in.SHA256URL != current.SHA256URL {
		if err := s.store.BumpProfileNodes(ctx, users); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func (s *Service) DeleteGeoSource(ctx context.Context, id int64) error {
	current, err := s.store.GetGeoSource(ctx, id)
	if err != nil {
		return err
	}
	users, _, err := s.profilesUsingSource(ctx, current.Name)
	if err != nil {
		return err
	}
	if len(users) > 0 {
		return &ConflictError{Field: "name", Message: "routing profiles refer to this source: change them first"}
	}
	return s.store.DeleteGeoSource(ctx, id)
}

func (s *Service) ListGeoSources(ctx context.Context) ([]store.GeoSource, error) {
	return s.store.ListGeoSources(ctx)
}

func (s *Service) RequestGeoRefresh(ctx context.Context) (int64, error) {
	return s.store.RequestGeoRefresh(ctx)
}

func ptrSlice(p *int64) []int64 {
	if p == nil {
		return nil
	}
	return []int64{*p}
}

func (s *Service) profileFitsNode(ctx context.Context, profileID, nodeID int64) error {
	row, err := s.store.GetProfile(ctx, profileID)
	if errors.Is(err, store.ErrNotFound) {
		return fieldErr("profile_id", "profile not found")
	}
	if err != nil {
		return err
	}
	p, err := parseProfile(row.Doc)
	if err != nil {
		return err
	}
	if slices.Contains(p.CascadeNodes(), nodeID) {
		return fieldErr("profile_id", "the profile cascades to this node: a node cannot cascade to itself")
	}
	return nil
}

func (s *Service) GetGeoSource(ctx context.Context, id int64) (*store.GeoSource, error) {
	return s.store.GetGeoSource(ctx, id)
}
