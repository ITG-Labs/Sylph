package agent

import (
	"context"
	"testing"
)

var _ XrayClient = newFakeXray()

func TestFakeXrayUserLifecycle(t *testing.T) {
	fx := newFakeXray()
	ctx := context.Background()
	if err := fx.AddUser(ctx, "vless-in", "u-1"); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "u-1") {
		t.Fatal("user not added")
	}
	if err := fx.RemoveUser(ctx, "vless-in", "u-1"); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("vless-in", "u-1") {
		t.Fatal("user not removed")
	}
}

func TestFakeXrayAddInboundValidation(t *testing.T) {
	fx := newFakeXray()
	if err := fx.AddInbound(context.Background(), `{"tag":"vless-in"}`); err != nil {
		t.Fatal(err)
	}
	if err := fx.AddInbound(context.Background(), `not-json`); err == nil {
		t.Fatal("bad json should fail")
	}
}
