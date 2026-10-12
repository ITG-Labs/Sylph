package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/logging"
)

type seen struct {
	method, path string
	body         map[string]any
}

func fakeCoreClient(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*CoreClient, *[]seen) {
	t.Helper()
	var calls []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := seen{method: r.Method, path: r.URL.Path}
		_ = json.NewDecoder(r.Body).Decode(&s.body)
		calls = append(calls, s)
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	api, err := apiclient.New("bot", srv.URL, nil, logging.NewWithWriter("bot", "error", io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return NewCoreClient(api), &calls
}

func TestCoreRegister(t *testing.T) {
	c, calls := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":7,"sub_token":"tok"}`)
	})
	u, err := c.Register(context.Background(), 1001)
	if err != nil || u.ID != 7 || u.SubToken != "tok" {
		t.Fatalf("user = %+v, %v", u, err)
	}
	got := (*calls)[0]
	if got.method != http.MethodPost || got.path != "/v1/users" || got.body["telegram_id"] != float64(1001) || len(got.body) != 1 {
		t.Fatalf("call = %+v", got)
	}
}

func TestCoreStatus(t *testing.T) {
	c, calls := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"user":{"id":7,"telegram_id":1001,"sub_token":"tok","status":"active"},
		 "subscription":{"id":3,"plan_id":2,"starts_at":"2026-10-01T10:00:00Z","expires_at":"2026-10-31T10:00:00Z","traffic_used_bytes":1073741824,"paid_until":"2026-11-30T10:00:00Z"},
		 "plan":{"id":2,"name":"Месяц","price_rub":199,"period_days":30,"traffic_limit_bytes":107374182400,"device_limit":3,"on_sale":true},
		 "devices":1}`)
	})
	st, err := c.Status(context.Background(), 1001)
	if err != nil {
		t.Fatal(err)
	}
	if st.User.ID != 7 || st.User.SubToken != "tok" || st.Subscription == nil || st.Subscription.PlanID != 2 || st.Subscription.TrafficUsedBytes != 1<<30 ||
		st.Subscription.PaidUntil == nil || st.Plan == nil || st.Plan.Name != "Месяц" || *st.Plan.TrafficLimitBytes != 100<<30 || *st.Plan.DeviceLimit != 3 || st.Devices != 1 {
		t.Fatalf("status = %+v", st)
	}
	if got := (*calls)[0]; got.method != http.MethodGet || got.path != "/v1/users/1001" {
		t.Fatalf("call = %+v", got)
	}
}

func TestCoreStatusWithoutSubscription(t *testing.T) {
	c, _ := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"user":{"id":7,"sub_token":"tok"},"subscription":null,"plan":null,"devices":0}`)
	})
	st, err := c.Status(context.Background(), 1001)
	if err != nil || st.Subscription != nil || st.Plan != nil {
		t.Fatalf("status = %+v, %v", st, err)
	}
}

func TestCorePlansPaymentsAndActions(t *testing.T) {
	c, calls := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/plans":
			io.WriteString(w, `[{"id":2,"name":"Месяц","price_rub":199,"period_days":30,"traffic_limit_bytes":null,"device_limit":null,"on_sale":true}]`)
		case "/v1/payments/create":
			io.WriteString(w, `{"payment_id":"2d85d5ad-000f-5000-9000-1c4c6dd1d9e6","confirmation_url":"https://yoomoney.ru/checkout","amount_rub":199}`)
		case "/v1/payments/2d85d5ad-000f-5000-9000-1c4c6dd1d9e6/check":
			io.WriteString(w, `{"status":"succeeded","applied":true,"telegram_id":1001,"paid_until":"2026-11-30T10:00:00Z"}`)
		case "/v1/users/7/rotate":
			io.WriteString(w, `{"sub_token":"new-tok"}`)
		case "/v1/users/7/devices/reset":
			io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	plans, err := c.Plans(ctx)
	if err != nil || len(plans) != 1 || plans[0].ID != 2 || plans[0].TrafficLimitBytes != nil || plans[0].PeriodDays != 30 {
		t.Fatalf("plans = %+v, %v", plans, err)
	}
	p, err := c.CreatePayment(ctx, 7, 2)
	if err != nil || p.PaymentID != "2d85d5ad-000f-5000-9000-1c4c6dd1d9e6" || p.ConfirmationURL != "https://yoomoney.ru/checkout" || p.AmountRub != 199 {
		t.Fatalf("payment = %+v, %v", p, err)
	}
	if b := (*calls)[1].body; b["user_id"] != float64(7) || b["plan_id"] != float64(2) || len(b) != 2 {
		t.Fatalf("create body = %v", b)
	}
	chk, err := c.CheckPayment(ctx, p.PaymentID)
	if err != nil || chk.Status != "succeeded" || !chk.Applied || chk.TelegramID == nil || *chk.TelegramID != 1001 || chk.PaidUntil == nil {
		t.Fatalf("check = %+v, %v", chk, err)
	}
	if got := (*calls)[2]; got.method != http.MethodPost {
		t.Fatalf("check call = %+v", got)
	}
	token, err := c.Rotate(ctx, 7)
	if err != nil || token != "new-tok" {
		t.Fatalf("rotate = %q, %v", token, err)
	}
	if err := c.ResetDevices(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[4]; got.method != http.MethodPost || got.path != "/v1/users/7/devices/reset" {
		t.Fatalf("reset call = %+v", got)
	}
}

func TestCoreCheckEscapesThePaymentID(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.EscapedPath()
		io.WriteString(w, `{"status":"pending"}`)
	}))
	t.Cleanup(srv.Close)
	api, _ := apiclient.New("bot", srv.URL, nil, logging.NewWithWriter("bot", "error", io.Discard))
	if _, err := NewCoreClient(api).CheckPayment(context.Background(), "../users/7/rotate"); err != nil {
		t.Fatal(err)
	}
	if raw != "/v1/payments/..%2Fusers%2F7%2Frotate/check" {
		t.Fatalf("path = %q", raw)
	}
}

func TestCoreErrorsMapToKinds(t *testing.T) {
	cases := map[string]struct {
		status int
		code   string
		want   error
	}{
		"not found": {http.StatusNotFound, "not_found", ErrNotFound},
		"conflict":  {http.StatusConflict, "conflict", ErrConflict},
		"disabled":  {http.StatusServiceUnavailable, "payments_disabled", ErrPaymentsDisabled},
		"provider":  {http.StatusBadGateway, "provider_unavailable", ErrProviderUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]string{"code": tc.code, "message": "x"})
			})
			if _, err := c.CreatePayment(context.Background(), 1, 1); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	c, _ := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	err := c.ResetDevices(context.Background(), 1)
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		t.Fatalf("500 err = %v", err)
	}
}
