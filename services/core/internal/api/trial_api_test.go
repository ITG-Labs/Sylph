package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func getJSON(t *testing.T, client *http.Client, url string, out any) int {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func postCall(t *testing.T, client *http.Client, url string, in any, out any) (int, string) {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return resp.StatusCode, e.Code
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, ""
}

func TestTrialEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleBot)
	ctx := context.Background()
	plan := &model.Plan{Name: "trial", PeriodDays: 3, Trial: true, HWIDRequired: true, LocationGroupID: testGroup(t, svc)}
	if err := svc.CreatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	u, _, err := svc.RegisterUser(ctx, 900)
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Trial *struct {
			ID           int64 `json:"id"`
			PeriodDays   int   `json:"period_days"`
			HWIDRequired bool  `json:"hwid_required"`
		} `json:"trial"`
		Subscription any `json:"subscription"`
	}
	if code := getJSON(t, client, ts.URL+"/v1/users/900", &st); code != http.StatusOK || st.Trial == nil || st.Trial.ID != plan.ID || !st.Trial.HWIDRequired {
		t.Fatalf("status before trial: %d %+v", code, st)
	}
	var got struct {
		PaidUntil time.Time `json:"paid_until"`
	}
	if code, _ := postCall(t, client, fmt.Sprintf("%s/v1/users/%d/trial", ts.URL, u.ID), nil, &got); code != http.StatusOK {
		t.Fatalf("grant = %d", code)
	}
	if d := time.Until(got.PaidUntil); d < 71*time.Hour || d > 73*time.Hour {
		t.Fatalf("paid_until = %v", got.PaidUntil)
	}
	st.Trial = nil
	if code := getJSON(t, client, ts.URL+"/v1/users/900", &st); code != http.StatusOK || st.Trial != nil || st.Subscription == nil {
		t.Fatalf("status after trial: %d %+v", code, st)
	}
	if code, ecode := postCall(t, client, fmt.Sprintf("%s/v1/users/%d/trial", ts.URL, u.ID), nil, nil); code != http.StatusConflict || ecode != "trial_unavailable" {
		t.Fatalf("second grant = %d %q", code, ecode)
	}
	if code, _ := postCall(t, client, ts.URL+"/v1/users/424242/trial", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown user = %d", code)
	}
	if code, _ := postCall(t, client, ts.URL+"/v1/users/abc/trial", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", code)
	}
}
