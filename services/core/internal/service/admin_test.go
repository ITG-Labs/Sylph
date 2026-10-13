package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestConcurrentInboundPatchesAreNotLost(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1", Address: "n1.example.com", Enabled: true}
	if err := svc.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateInbound(ctx, n.ID, inbound.Spec{Tag: "main", Port: 443, Label: "start", Reality: &inbound.Reality{Dest: "www.example.com:443", ServerNames: []string{"www.example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 20; round++ {
		label := fmt.Sprintf("label-%d", round)
		empty := ""
		var wg sync.WaitGroup
		var rotated *InboundView
		var rotateErr, labelErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, labelErr = svc.UpdateInbound(ctx, created.ID, InboundPatch{Label: &label})
		}()
		go func() {
			defer wg.Done()
			rotated, rotateErr = svc.UpdateInbound(ctx, created.ID, InboundPatch{Reality: &RealityPatch{PrivateKey: &empty}})
		}()
		wg.Wait()
		if labelErr != nil || rotateErr != nil {
			t.Fatalf("round %d: label err %v, rotate err %v", round, labelErr, rotateErr)
		}
		list, err := svc.ListInbounds(ctx, n.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := list[0]
		if got.Label != label {
			t.Fatalf("round %d: label %q lost, got %q", round, label, got.Label)
		}
		if got.Reality.PublicKey != rotated.Reality.PublicKey {
			t.Fatalf("round %d: rotation answered %s but %s is stored", round, rotated.Reality.PublicKey, got.Reality.PublicKey)
		}
	}
}

func TestNamesRejectSpacesAndControlCharacters(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	var fe *FieldError
	for _, name := range []string{"   ", "a\x00b", "bell\x07", " "} {
		if _, err := svc.CreateGroup(ctx, name); !errors.As(err, &fe) || fe.Field != "name" {
			t.Fatalf("group %q: %v", name, err)
		}
		if err := svc.CreatePlanChecked(ctx, &model.Plan{Name: name, PriceRub: 1, PeriodDays: 30, Visibility: model.VisibilityPublic}); !errors.As(err, &fe) || fe.Field != "name" {
			t.Fatalf("plan %q: %v", name, err)
		}
		if err := svc.CreateNode(ctx, &model.Node{CertCN: "n-1", Name: name, Address: "n1.example.com"}); !errors.As(err, &fe) || fe.Field != "name" {
			t.Fatalf("node %q: %v", name, err)
		}
	}
	if _, err := svc.CreateGroup(ctx, "Германия 🇩🇪"); err != nil {
		t.Fatalf("normal name rejected: %v", err)
	}
}

func TestDestCannotPointAtANode(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	a := &model.Node{CertCN: "node-a", Name: "a", Address: "de1.example.com", Enabled: true}
	if err := svc.CreateNode(ctx, a); err != nil {
		t.Fatal(err)
	}
	b := &model.Node{CertCN: "node-b", Name: "b", Address: "203.0.113.7", Enabled: true}
	if err := svc.CreateNode(ctx, b); err != nil {
		t.Fatal(err)
	}
	var fe *FieldError
	for _, dest := range []string{"de1.example.com:443", "DE1.EXAMPLE.COM:8443", "203.0.113.7:443"} {
		_, err := svc.CreateInbound(ctx, b.ID, inbound.Spec{Tag: "main", Port: 443, Reality: &inbound.Reality{Dest: dest, ServerNames: []string{"www.example.com"}}})
		if !errors.As(err, &fe) || fe.Field != "reality.dest" {
			t.Fatalf("dest %s: %v", dest, err)
		}
	}
	in, err := svc.CreateInbound(ctx, a.ID, inbound.Spec{Tag: "main", Port: 443, Reality: &inbound.Reality{Dest: "www.example.com:443", ServerNames: []string{"www.example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	dest := "de1.example.com:443"
	if _, err := svc.UpdateInbound(ctx, in.ID, InboundPatch{Reality: &RealityPatch{Dest: &dest}}); !errors.As(err, &fe) || fe.Field != "reality.dest" {
		t.Fatalf("patch dest to a node: %v", err)
	}
	if err := svc.CreateNode(ctx, &model.Node{CertCN: "node-c", Name: "c", Address: "WWW.example.com"}); !errors.As(err, &fe) || fe.Field != "address" {
		t.Fatalf("node at an inbound dest: %v", err)
	}
	addr := "www.example.com"
	if _, err := svc.UpdateNode(ctx, b.ID, NodePatch{Address: &addr}); !errors.As(err, &fe) || fe.Field != "address" {
		t.Fatalf("node moved to an inbound dest: %v", err)
	}
	if _, err := svc.UpdateNode(ctx, a.ID, NodePatch{Address: &[]string{"de1.example.com"}[0]}); err != nil {
		t.Fatalf("node keeps its own address: %v", err)
	}
}

func TestConcurrentInboundsWithTheSameSNI(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1", Address: "n1.example.com", Enabled: true}
	if err := svc.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	pool := poolOf(svc)
	conns := make([]*pgxpool.Conn, 0, pool.Config().MaxConns)
	for range pool.Config().MaxConns {
		c, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		c.Release()
	}
	const workers = 8
	start := make(chan struct{})
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			<-start
			_, errs[i] = svc.CreateInbound(ctx, n.ID, inbound.Spec{Tag: fmt.Sprintf("in-%d", i), Port: 443,
				Reality: &inbound.Reality{Dest: "www.example.com:443", ServerNames: []string{"www.example.com"}}})
		})
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, err := range errs {
		var ce *ConflictError
		switch {
		case err == nil:
			ok++
		case !errors.As(err, &ce) || ce.Field != "reality.server_names":
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d inbounds with the same SNI were created", ok)
	}
}
