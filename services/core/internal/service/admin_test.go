package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

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
