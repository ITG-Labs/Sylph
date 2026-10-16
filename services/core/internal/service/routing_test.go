package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func docOf(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func basicDoc() map[string]any {
	return map[string]any{
		"outbounds": []any{
			map[string]any{"tag": "direct", "kind": "direct"},
			map[string]any{"tag": "block", "kind": "block"},
		},
		"rules":   []any{map[string]any{"name": "Торренты", "protocol": []string{"bittorrent"}, "to": "block"}},
		"default": "direct",
	}
}

func mustField(t *testing.T, err error, field string) {
	t.Helper()
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("err = %v, want field %q", err, field)
	}
}

func mustConflict(t *testing.T, err error, field string) {
	t.Helper()
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Field != field {
		t.Fatalf("err = %v, want conflict on %q", err, field)
	}
}

type routingFixture struct {
	svc  *Service
	x, y *model.Node
	key  string
	pub  string
}

func realityNode(t *testing.T, svc *Service, cn string) (*model.Node, *InboundView) {
	t.Helper()
	ctx := context.Background()
	n := &model.Node{CertCN: cn, Name: strings.ToUpper(cn), Address: cn + ".example.com", Enabled: true}
	if err := svc.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	in, err := svc.CreateInbound(ctx, n.ID, inbound.Spec{Tag: "main", Port: 443, Label: "R",
		Reality: &inbound.Reality{Dest: "www.microsoft.com:443", ServerNames: []string{"www.microsoft.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	return n, in
}

func cascadeDocTo(id int64) map[string]any {
	d := basicDoc()
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{"tag": "to-exit", "kind": "cascade", "cascade": map[string]any{"node_id": id}})
	d["default"] = "to-exit"
	return d
}

func TestProfileChecksAndReferences(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	if _, err := svc.CreateProfile(ctx, "base", docOf(t, basicDoc())); err != nil {
		t.Fatal(err)
	}
	mustConflict(t, func() error { _, err := svc.CreateProfile(ctx, "base", docOf(t, basicDoc())); return err }(), "name")
	_, err := svc.CreateProfile(ctx, " ", docOf(t, basicDoc()))
	mustField(t, err, "name")
	_, err = svc.CreateProfile(ctx, "x", json.RawMessage(`{"outbounds":`))
	mustField(t, err, "doc")
	_, err = svc.CreateProfile(ctx, "x", json.RawMessage(`{"default":"direct","extra":1}`))
	mustField(t, err, "doc")
	d := basicDoc()
	d["outbounds"].([]any)[0].(map[string]any)["tag"] = "Bad!"
	_, err = svc.CreateProfile(ctx, "x", docOf(t, d))
	mustField(t, err, "doc.outbounds[0].tag")
	d = basicDoc()
	d["rules"] = []any{map[string]any{"plans": []int64{999}, "to": "block"}}
	_, err = svc.CreateProfile(ctx, "x", docOf(t, d))
	mustField(t, err, "doc.rules[0].plans")
	_, err = svc.CreateProfile(ctx, "x", docOf(t, cascadeDocTo(999)))
	mustField(t, err, "doc.outbounds[2].cascade.node_id")
	d = basicDoc()
	d["rules"] = []any{map[string]any{"domain": []string{"ru-blocked:ru"}, "to": "block"}}
	_, err = svc.CreateProfile(ctx, "x", docOf(t, d))
	mustField(t, err, "doc.rules[0].domain")
	if _, err := svc.CreateGeoSource(ctx, GeoSourceInput{Name: "ru-blocked", Kind: "ip", URL: "https://example.com/geoip.dat"}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateProfile(ctx, "x", docOf(t, d))
	mustField(t, err, "doc.rules[0].domain")
	d["rules"] = []any{map[string]any{"ip": []string{"ru-blocked:ru"}, "to": "block"}}
	if _, err := svc.CreateProfile(ctx, "x", docOf(t, d)); err != nil {
		t.Fatal(err)
	}
}

func TestProfileSecretsAreWriteOnly(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	d := basicDoc()
	proxy := map[string]any{"protocol": "trojan", "address": "fi.example.com", "port": 443, "password": "hunter2"}
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{"tag": "fi", "kind": "proxy", "proxy": proxy})
	v, err := svc.CreateProfile(ctx, "p", docOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(v.Doc), "hunter2") || !strings.Contains(string(v.Doc), `"has_secret":true`) {
		t.Fatalf("view leaks or hides the secret flag: %s", v.Doc)
	}
	proxy["password"] = ""
	v, err = svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := svc.store.GetProfile(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored.Doc), "hunter2") {
		t.Fatalf("empty secret did not keep the old one: %s", stored.Doc)
	}
	proxy["password"] = "new-pass"
	if v, err = svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision); err != nil {
		t.Fatal(err)
	}
	stored, _ = svc.store.GetProfile(ctx, v.ID)
	if !strings.Contains(string(stored.Doc), "new-pass") {
		t.Fatalf("secret not replaced: %s", stored.Doc)
	}
	proxy["password"] = ""
	d["outbounds"].([]any)[2].(map[string]any)["tag"] = "fi-2"
	_, err = svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision)
	mustField(t, err, "doc.outbounds[2].proxy.password")
	d["outbounds"].([]any)[2].(map[string]any)["tag"] = "fi"
	_, err = svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision-1)
	mustConflict(t, err, "revision")
}

func setupCascade(t *testing.T) *routingFixture {
	t.Helper()
	svc := setupService(t)
	ctx := context.Background()
	x, _ := realityNode(t, svc, "node-x")
	y, yIn := realityNode(t, svc, "node-y")
	p, err := svc.CreateProfile(ctx, "bridge", docOf(t, cascadeDocTo(y.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateNode(ctx, x.ID, NodePatch{ProfileID: Optional[int64]{Set: true, Value: &p.ID}}); err != nil {
		t.Fatal(err)
	}
	key, err := svc.store.LinkKey(ctx, x.ID, y.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &routingFixture{svc: svc, x: x, y: y, key: key, pub: yIn.Reality.PublicKey}
}

func TestCascadeIsResolvedInSync(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	resp, err := f.svc.SyncNodeUsers(ctx, f.x.CertCN, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Routing == nil {
		t.Fatal("no routing in sync")
	}
	var exit *noderoute.Outbound
	for i := range resp.Routing.Outbounds {
		if resp.Routing.Outbounds[i].Tag == "to-exit" {
			exit = &resp.Routing.Outbounds[i]
		}
	}
	if exit == nil || exit.Kind != noderoute.KindProxy || exit.Proxy.Address != "node-y.example.com" || exit.Proxy.Port != 443 ||
		exit.Proxy.UUID != f.key || exit.Proxy.PublicKey != f.pub || exit.Proxy.SNI != "www.microsoft.com" ||
		exit.Proxy.Flow != inbound.FlowVision || exit.Proxy.Security != "reality" || exit.Proxy.Fingerprint != "chrome" {
		t.Fatalf("cascade outbound = %+v %+v", exit, exit.Proxy)
	}
	if err := noderoute.ValidateResolved(resp.Routing); err != nil {
		t.Fatalf("resolved routing is invalid: %v", err)
	}
	exitResp, err := f.svc.SyncNodeUsers(ctx, f.y.CertCN, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(exitResp.ServiceKeys) != 1 || exitResp.ServiceKeys[0] != f.key || exitResp.Routing != nil {
		t.Fatalf("exit sync = keys %v routing %+v", exitResp.ServiceKeys, exitResp.Routing)
	}
}

func TestCascadeToItselfIsRefused(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	p, err := f.svc.store.GetNode(ctx, f.x.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.UpdateNode(ctx, f.y.ID, NodePatch{ProfileID: Optional[int64]{Set: true, Value: p.ProfileID}})
	mustField(t, err, "profile_id")
	view, err := f.svc.GetProfile(ctx, *p.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.UpdateProfile(ctx, view.ID, view.Name, docOf(t, cascadeDocTo(f.x.ID)), view.Revision)
	mustField(t, err, "doc.outbounds[2].cascade.node_id")
}

func TestCascadeToUnavailableNodeBecomesBlock(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	off := false
	v := f.svc.store
	before, _ := v.GetNode(ctx, f.x.ID)
	if _, err := f.svc.UpdateNode(ctx, f.y.ID, NodePatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	after, _ := v.GetNode(ctx, f.x.ID)
	if after.UsersVersion <= before.UsersVersion {
		t.Fatal("disabling the exit did not bump the cascading node")
	}
	resp, err := f.svc.SyncNodeUsers(ctx, f.x.CertCN, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range resp.Routing.Outbounds {
		if o.Tag == "to-exit" && o.Kind != noderoute.KindBlock {
			t.Fatalf("unavailable cascade = %+v", o)
		}
	}
	view, err := f.svc.GetProfile(ctx, *after.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Warnings) != 1 || view.Warnings[0].Outbound != "to-exit" || view.Warnings[0].Problem != "disabled" || view.Warnings[0].NodeID != f.y.ID {
		t.Fatalf("warnings = %+v", view.Warnings)
	}
}

func TestExitInboundChangeBumpsCascadingNode(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	list, err := f.svc.ListInbounds(ctx, f.y.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := f.svc.store.GetNode(ctx, f.x.ID)
	empty := ""
	if _, err := f.svc.UpdateInbound(ctx, list[0].ID, InboundPatch{Reality: &RealityPatch{PrivateKey: &empty}}); err != nil {
		t.Fatal(err)
	}
	after, _ := f.svc.store.GetNode(ctx, f.x.ID)
	if after.UsersVersion <= before.UsersVersion {
		t.Fatal("rekeying the exit did not bump the cascading node")
	}
}

func TestSyncCarriesPlansAndGeo(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	n, _ := realityNode(t, svc, "node-g")
	plan := &model.Plan{Name: "Премиум", PriceRub: 1, PeriodDays: 30, Visibility: model.VisibilityPublic}
	if err := svc.CreatePlanChecked(ctx, plan); err != nil {
		t.Fatal(err)
	}
	u, key, err := svc.RegisterUser(ctx, 7101)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.ApplyPayment(ctx, u.ID, plan.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GeoSourceInput{{Name: "ru-blocked", Kind: "domain", URL: "https://example.com/geosite.dat"}, {Name: "unused", Kind: "ip", URL: "https://example.com/geoip.dat"}} {
		if _, err := svc.CreateGeoSource(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	d := basicDoc()
	d["rules"] = []any{
		map[string]any{"domain": []string{"ru-blocked:ru"}, "to": "direct"},
		map[string]any{"plans": []int64{plan.ID}, "to": "block"},
	}
	p, err := svc.CreateProfile(ctx, "p", docOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateNode(ctx, n.ID, NodePatch{ProfileID: Optional[int64]{Set: true, Value: &p.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RequestGeoRefresh(ctx); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.SyncNodeUsers(ctx, n.CertCN, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Users) != 1 || resp.Users[0].UUID != key || resp.Users[0].PlanID != plan.ID {
		t.Fatalf("users = %+v", resp.Users)
	}
	if resp.Geo == nil || resp.Geo.Generation != 1 || len(resp.Geo.Sources) != 1 || resp.Geo.Sources[0].Name != "ru-blocked" || resp.Geo.Sources[0].Kind != "domain" {
		t.Fatalf("geo = %+v", resp.Geo)
	}
}

func TestGeoSourceRules(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	_, err := svc.CreateGeoSource(ctx, GeoSourceInput{Name: "geoip", Kind: "ip", URL: "https://example.com/a.dat"})
	mustField(t, err, "name")
	_, err = svc.CreateGeoSource(ctx, GeoSourceInput{Name: "x", Kind: "ip", URL: "http://example.com/a.dat"})
	mustField(t, err, "url")
	_, err = svc.CreateGeoSource(ctx, GeoSourceInput{Name: "x", Kind: "ip", URL: "https://10.0.0.1/a.dat"})
	mustField(t, err, "url")
	_, err = svc.CreateGeoSource(ctx, GeoSourceInput{Name: "x", Kind: "ip", URL: "https://example.com/a.dat", SHA256URL: "ftp://example.com/a"})
	mustField(t, err, "sha256_url")
	_, err = svc.CreateGeoSource(ctx, GeoSourceInput{Name: "x", Kind: "mixed", URL: "https://example.com/a.dat"})
	mustField(t, err, "kind")
	g, err := svc.CreateGeoSource(ctx, GeoSourceInput{Name: "ru", Kind: "domain", URL: "https://example.com/a.dat"})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := realityNode(t, svc, "node-s")
	d := basicDoc()
	d["rules"] = []any{map[string]any{"domain": []string{"ru:ru"}, "to": "direct"}}
	p, err := svc.CreateProfile(ctx, "p", docOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateNode(ctx, n.ID, NodePatch{ProfileID: Optional[int64]{Set: true, Value: &p.ID}}); err != nil {
		t.Fatal(err)
	}
	mustConflict(t, svc.DeleteGeoSource(ctx, g.ID), "name")
	_, err = svc.UpdateGeoSource(ctx, g.ID, GeoSourceInput{Name: "ru-2", Kind: "domain", URL: g.URL})
	mustConflict(t, err, "name")
	_, err = svc.UpdateGeoSource(ctx, g.ID, GeoSourceInput{Name: "ru", Kind: "ip", URL: g.URL})
	mustConflict(t, err, "kind")
	before, _ := svc.store.GetNode(ctx, n.ID)
	if _, err := svc.UpdateGeoSource(ctx, g.ID, GeoSourceInput{Name: "ru", Kind: "domain", URL: "https://mirror.example.com/a.dat"}); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.store.GetNode(ctx, n.ID)
	if after.UsersVersion <= before.UsersVersion {
		t.Fatal("changing a used source did not bump the node")
	}
}

func TestDeleteProfileOnlyUnused(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	x, _ := f.svc.store.GetNode(ctx, f.x.ID)
	mustConflict(t, f.svc.DeleteProfile(ctx, *x.ProfileID), "profile")
	if _, err := f.svc.UpdateNode(ctx, f.x.ID, NodePatch{ProfileID: Optional[int64]{Set: true}}); err != nil {
		t.Fatal(err)
	}
	if keys, _ := f.svc.store.ServiceKeys(ctx, f.y.ID); len(keys) != 0 {
		t.Fatalf("link kept after unassigning: %v", keys)
	}
	if err := f.svc.DeleteProfile(ctx, *x.ProfileID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteProfile(ctx, *x.ProfileID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestPreviewHidesSecrets(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	x, _ := f.svc.store.GetNode(ctx, f.x.ID)
	raw, err := f.svc.PreviewProfile(ctx, *x.ProfileID, &f.x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), f.key) || !strings.Contains(string(raw), `"***"`) || !strings.Contains(string(raw), "node-y.example.com") {
		t.Fatalf("preview = %s", raw)
	}
	if raw, err = f.svc.PreviewProfile(ctx, *x.ProfileID, nil); err != nil || !strings.Contains(string(raw), "blackhole") {
		t.Fatalf("preview without a node = %s %v", raw, err)
	}
}

func TestSecretIsNotKeptForANewDestination(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	d := basicDoc()
	proxy := map[string]any{"protocol": "socks", "address": "socks.example.com", "port": 1080, "username": "u", "password": "hunter2"}
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{"tag": "s", "kind": "proxy", "proxy": proxy})
	v, err := svc.CreateProfile(ctx, "p", docOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	proxy["password"] = ""
	for _, change := range []func(){
		func() { proxy["address"] = "evil.example.net" },
		func() { proxy["address"] = "socks.example.com"; proxy["port"] = 2080 },
		func() { proxy["port"] = 1080; proxy["username"] = "other" },
		func() {
			proxy["username"] = "u"
			proxy["security"] = ""
			proxy["protocol"] = "http"
			proxy["port"] = 1080
		},
	} {
		change()
		_, err := svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision)
		mustField(t, err, "doc.outbounds[2].proxy.password")
	}
	proxy["username"], proxy["protocol"] = "u", "socks"
	delete(proxy, "security")
	if _, err := svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision); err != nil {
		t.Fatalf("same destination must keep the secret: %v", err)
	}
}

func TestDisabledEntryNodeLosesItsServiceKey(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	off, on := false, true
	if _, err := f.svc.UpdateNode(ctx, f.x.ID, NodePatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if keys, _ := f.svc.store.ServiceKeys(ctx, f.y.ID); len(keys) != 0 {
		t.Fatalf("disabled entry node keeps its key on the exit: %v", keys)
	}
	before, err := f.svc.store.GetNode(ctx, f.x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateNode(ctx, f.x.ID, NodePatch{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	keys, _ := f.svc.store.ServiceKeys(ctx, f.y.ID)
	if len(keys) != 1 || keys[0] == f.key {
		t.Fatalf("re-enabled node must get a fresh key: %v (old %s)", keys, f.key)
	}
	after, err := f.svc.store.GetNode(ctx, f.x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.UsersVersion == before.UsersVersion {
		t.Fatal("the re-enabled entry node was not told about its new service key")
	}
	resp, err := f.svc.SyncNodeUsers(ctx, f.x.CertCN, before.UsersVersion)
	if err != nil || resp.Routing == nil {
		t.Fatalf("sync: %v", err)
	}
	for _, o := range resp.Routing.Outbounds {
		if o.Proxy != nil && o.Proxy.UUID != keys[0] {
			t.Fatalf("entry node cascades with %s, the exit knows %s", o.Proxy.UUID, keys[0])
		}
	}
}

func TestSyncFailsInsteadOfDroppingTheProfile(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	if _, err := poolOf(f.svc).Exec(ctx, `UPDATE routing_profiles SET doc = '{"outbounds": 1}'`); err != nil {
		t.Fatal(err)
	}
	if resp, err := f.svc.SyncNodeUsers(ctx, f.x.CertCN, 0); err == nil {
		t.Fatalf("sync answered without the node's profile: routing %v", resp.Routing)
	}
}

func TestSecretIsNotKeptWhenTLSIsDropped(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	d := basicDoc()
	proxy := map[string]any{"protocol": "http", "address": "proxy.example.com", "port": 3128, "username": "u", "password": "hunter2", "security": "tls", "sni": "proxy.example.com"}
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{"tag": "h", "kind": "proxy", "proxy": proxy})
	v, err := svc.CreateProfile(ctx, "p", docOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	proxy["password"] = ""
	for _, change := range []func(){
		func() { proxy["security"] = "" },
		func() { proxy["security"] = "tls"; proxy["sni"] = "evil.example.net" },
	} {
		change()
		_, err := svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision)
		mustField(t, err, "doc.outbounds[2].proxy.password")
	}
}

func TestRawSecretsAreHiddenAndKeptOnlyUnchanged(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	d := basicDoc()
	raw := map[string]any{"protocol": "wireguard", "settings": map[string]any{"secretKey": "wg-secret", "address": []string{"172.16.0.2/32"},
		"peers": []any{map[string]any{"publicKey": "pub", "endpoint": "engage.cloudflareclient.com:2408"}}}}
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{"tag": "warp", "kind": "raw", "raw": raw})
	v, err := svc.CreateProfile(ctx, "p", docOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(v.Doc), "wg-secret") || !strings.Contains(string(v.Doc), `"secretKey":"***"`) || !strings.Contains(string(v.Doc), `"publicKey":"pub"`) {
		t.Fatalf("view = %s", v.Doc)
	}
	n, _ := realityNode(t, svc, "node-w")
	if _, err := svc.UpdateNode(ctx, n.ID, NodePatch{ProfileID: Optional[int64]{Set: true, Value: &v.ID}}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewProfile(ctx, v.ID, &n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(preview), "wg-secret") {
		t.Fatalf("preview leaks: %s", preview)
	}
	var masked map[string]any
	if err := json.Unmarshal(v.Doc, &masked); err != nil {
		t.Fatal(err)
	}
	if v, err = svc.UpdateProfile(ctx, v.ID, "p", docOf(t, masked), v.Revision); err != nil {
		t.Fatalf("unchanged masked raw: %v", err)
	}
	stored, _ := svc.store.GetProfile(ctx, v.ID)
	if !strings.Contains(string(stored.Doc), "wg-secret") {
		t.Fatalf("secret lost: %s", stored.Doc)
	}
	peers := masked["outbounds"].([]any)[2].(map[string]any)["raw"].(map[string]any)["settings"].(map[string]any)["peers"].([]any)
	peers[0].(map[string]any)["endpoint"] = "evil.example.net:2408"
	_, err = svc.UpdateProfile(ctx, v.ID, "p", docOf(t, masked), v.Revision)
	mustField(t, err, "doc.outbounds[2].raw")
	resp, err := svc.SyncNodeUsers(ctx, n.CertCN, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp.Routing.Outbounds[2].Raw), "wg-secret") {
		t.Fatalf("node did not get the real secret: %s", resp.Routing.Outbounds[2].Raw)
	}
}

func TestSecretIsNotKeptWhenRealityKeyChanges(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	d := basicDoc()
	proxy := map[string]any{"protocol": "vless", "address": "nl.example.com", "port": 443, "uuid": "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b",
		"security": "reality", "flow": "xtls-rprx-vision", "sni": "www.microsoft.com", "public_key": "Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw", "short_id": "ab12"}
	d["outbounds"] = append(d["outbounds"].([]any), map[string]any{"tag": "nl", "kind": "proxy", "proxy": proxy})
	v, err := svc.CreateProfile(ctx, "p", docOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	proxy["uuid"] = ""
	if _, err := svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision); err != nil {
		t.Fatalf("unchanged outbound must keep the uuid (defaults like the fingerprint are filled in): %v", err)
	}
	v, _ = svc.GetProfile(ctx, v.ID)
	for _, change := range []func(){
		func() { proxy["public_key"] = "MDHqaJPhoMxwOUVfWKd2y8WK8XFd8mmQr0LHNlU2ZVE" },
		func() { proxy["public_key"] = "Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw"; proxy["short_id"] = "cd" },
		func() { proxy["short_id"] = "ab12"; proxy["fingerprint"] = "firefox" },
	} {
		change()
		_, err := svc.UpdateProfile(ctx, v.ID, "p", docOf(t, d), v.Revision)
		mustField(t, err, "doc.outbounds[2].proxy.uuid")
	}
}
