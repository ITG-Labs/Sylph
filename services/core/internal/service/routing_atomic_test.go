package service

import (
	"context"
	"testing"
)

func failOn(t *testing.T, svc *Service, trigger, table, event, cond string) func() {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`CREATE FUNCTION fail_it() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$ LANGUAGE plpgsql`,
		`CREATE TRIGGER ` + trigger + ` BEFORE ` + event + ` ON ` + table + ` FOR EACH ROW ` + cond + ` EXECUTE FUNCTION fail_it()`,
	} {
		if _, err := poolOf(svc).Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		if _, err := poolOf(svc).Exec(ctx, `DROP TRIGGER `+trigger+` ON `+table); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNodeProfileChangeIsAllOrNothing(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	x, _ := realityNode(t, svc, "node-x")
	y, _ := realityNode(t, svc, "node-y")
	p, err := svc.CreateProfile(ctx, "bridge", docOf(t, cascadeDocTo(y.ID)))
	if err != nil {
		t.Fatal(err)
	}
	heal := failOn(t, svc, "fail_links", "node_links", "INSERT", "")
	patch := NodePatch{ProfileID: Optional[int64]{Set: true, Value: &p.ID}}
	if _, err := svc.UpdateNode(ctx, x.ID, patch); err == nil {
		t.Fatal("the failure was swallowed")
	}
	if n, err := svc.store.GetNode(ctx, x.ID); err != nil || n.ProfileID != nil {
		t.Fatalf("the profile stayed set although the links could not be built: %+v %v", n, err)
	}
	heal()
	if _, err := svc.UpdateNode(ctx, x.ID, patch); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.LinkKey(ctx, x.ID, y.ID); err != nil {
		t.Fatalf("the repeated request did not build the link: %v", err)
	}
}

func TestProfileUpdateIsAllOrNothing(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	x, _ := realityNode(t, svc, "node-x")
	y, _ := realityNode(t, svc, "node-y")
	base, err := svc.CreateProfile(ctx, "bridge", docOf(t, basicDoc()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateNode(ctx, x.ID, NodePatch{ProfileID: Optional[int64]{Set: true, Value: &base.ID}}); err != nil {
		t.Fatal(err)
	}
	heal := failOn(t, svc, "fail_links", "node_links", "INSERT", "")
	doc := docOf(t, cascadeDocTo(y.ID))
	if _, err := svc.UpdateProfile(ctx, base.ID, "bridge", doc, base.Revision); err == nil {
		t.Fatal("the failure was swallowed")
	}
	if row, err := svc.store.GetProfile(ctx, base.ID); err != nil || row.Revision != base.Revision {
		t.Fatalf("the profile changed although the links could not be built: %+v %v", row, err)
	}
	heal()
	if _, err := svc.UpdateProfile(ctx, base.ID, "bridge", doc, base.Revision); err != nil {
		t.Fatalf("the repeated request was refused: %v", err)
	}
	if _, err := svc.store.LinkKey(ctx, x.ID, y.ID); err != nil {
		t.Fatalf("the repeated request did not build the link: %v", err)
	}
}

func TestGeoSourceChangeIsAllOrNothing(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
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
	heal := failOn(t, svc, "fail_bump", "nodes", "UPDATE", "WHEN (OLD.users_version IS DISTINCT FROM NEW.users_version)")
	in := GeoSourceInput{Name: "ru", Kind: "domain", URL: "https://mirror.example.com/a.dat"}
	if _, err := svc.UpdateGeoSource(ctx, g.ID, in); err == nil {
		t.Fatal("the failure was swallowed")
	}
	if cur, err := svc.store.GetGeoSource(ctx, g.ID); err != nil || cur.URL != g.URL {
		t.Fatalf("the url changed although the nodes were not told: %+v %v", cur, err)
	}
	heal()
	before, _ := svc.store.GetNode(ctx, n.ID)
	if _, err := svc.UpdateGeoSource(ctx, g.ID, in); err != nil {
		t.Fatal(err)
	}
	if after, _ := svc.store.GetNode(ctx, n.ID); after.UsersVersion <= before.UsersVersion {
		t.Fatal("the repeated request did not bump the node")
	}
}
