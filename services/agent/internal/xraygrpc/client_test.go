package xraygrpc

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/protocol"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/proxymancmd"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/statscmd"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/vless"
)

const testUUID = "6f1c2c1e-3b1a-4c55-9d5e-0a1b2c3d4e5f"

type fakeXray struct {
	proxymancmd.UnimplementedHandlerServiceServer
	statscmd.UnimplementedStatsServiceServer
	mu       sync.Mutex
	alters   []*proxymancmd.AlterInboundRequest
	alterErr error
	lists    []*proxymancmd.GetInboundUserRequest
	users    []*protocol.User
	listErr  error
	queries  []*statscmd.QueryStatsRequest
	stats    []*statscmd.Stat
	statsErr error
	uptime   uint32
	sysErr   error
	block    bool
}

func (f *fakeXray) wait(ctx context.Context) error {
	f.mu.Lock()
	block := f.block
	f.mu.Unlock()
	if !block {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeXray) AlterInbound(ctx context.Context, req *proxymancmd.AlterInboundRequest) (*proxymancmd.AlterInboundResponse, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alters = append(f.alters, req)
	if f.alterErr != nil {
		return nil, f.alterErr
	}
	return &proxymancmd.AlterInboundResponse{}, nil
}

func (f *fakeXray) GetInboundUsers(ctx context.Context, req *proxymancmd.GetInboundUserRequest) (*proxymancmd.GetInboundUserResponse, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists = append(f.lists, req)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &proxymancmd.GetInboundUserResponse{Users: f.users}, nil
}

func (f *fakeXray) QueryStats(ctx context.Context, req *statscmd.QueryStatsRequest) (*statscmd.QueryStatsResponse, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, req)
	if f.statsErr != nil {
		return nil, f.statsErr
	}
	return &statscmd.QueryStatsResponse{Stat: f.stats}, nil
}

func (f *fakeXray) GetSysStats(ctx context.Context, req *statscmd.SysStatsRequest) (*statscmd.SysStatsResponse, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sysErr != nil {
		return nil, f.sysErr
	}
	return &statscmd.SysStatsResponse{Uptime: f.uptime, NumGoroutine: 7}, nil
}

func (f *fakeXray) queryCalls() []*statscmd.QueryStatsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*statscmd.QueryStatsRequest(nil), f.queries...)
}

func (f *fakeXray) alterCalls() []*proxymancmd.AlterInboundRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*proxymancmd.AlterInboundRequest(nil), f.alters...)
}

func (f *fakeXray) listCalls() []*proxymancmd.GetInboundUserRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*proxymancmd.GetInboundUserRequest(nil), f.lists...)
}

func startFake(t *testing.T, fx *fakeXray, timeout time.Duration) *Client {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	proxymancmd.RegisterHandlerServiceServer(srv, fx)
	statscmd.RegisterStatsServiceServer(srv, fx)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	c, err := dial("passthrough:///bufnet", timeout, grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func decodeAddUser(t *testing.T, req *proxymancmd.AlterInboundRequest) (*protocol.User, *vless.Account) {
	t.Helper()
	if req.GetOperation().GetType() != "xray.app.proxyman.command.AddUserOperation" {
		t.Fatalf("operation type = %q", req.GetOperation().GetType())
	}
	var op proxymancmd.AddUserOperation
	if err := proto.Unmarshal(req.GetOperation().GetValue(), &op); err != nil {
		t.Fatal(err)
	}
	if op.GetUser().GetAccount().GetType() != "xray.proxy.vless.Account" {
		t.Fatalf("account type = %q", op.GetUser().GetAccount().GetType())
	}
	var acc vless.Account
	if err := proto.Unmarshal(op.GetUser().GetAccount().GetValue(), &acc); err != nil {
		t.Fatal(err)
	}
	return op.GetUser(), &acc
}

func TestAddUserSendsVlessAccount(t *testing.T) {
	fx := &fakeXray{}
	c := startFake(t, fx, time.Second)
	if err := c.AddUser(context.Background(), "vless-in", testUUID, testUUID, "xtls-rprx-vision"); err != nil {
		t.Fatal(err)
	}
	calls := fx.alterCalls()
	if len(calls) != 1 {
		t.Fatalf("alter calls = %d, want 1", len(calls))
	}
	if calls[0].GetTag() != "vless-in" {
		t.Fatalf("tag = %q", calls[0].GetTag())
	}
	user, acc := decodeAddUser(t, calls[0])
	if user.GetEmail() != testUUID || user.GetLevel() != 0 {
		t.Fatalf("user = %v", user)
	}
	if acc.GetId() != testUUID || acc.GetFlow() != "xtls-rprx-vision" || acc.GetEncryption() != "" {
		t.Fatalf("account = %v", acc)
	}
}

func TestAddUserEmptyFlow(t *testing.T) {
	fx := &fakeXray{}
	c := startFake(t, fx, time.Second)
	if err := c.AddUser(context.Background(), "vless-in", testUUID, testUUID, ""); err != nil {
		t.Fatal(err)
	}
	_, acc := decodeAddUser(t, fx.alterCalls()[0])
	if acc.GetFlow() != "" {
		t.Fatalf("flow = %q, want empty", acc.GetFlow())
	}
}

func TestAddUserAlreadyExistsIsSuccess(t *testing.T) {
	fx := &fakeXray{alterErr: errors.New("proxy/vless: User " + testUUID + " already exists.")}
	c := startFake(t, fx, time.Second)
	if err := c.AddUser(context.Background(), "vless-in", testUUID, testUUID, ""); err != nil {
		t.Fatalf("already exists must be success, got %v", err)
	}
}

func TestAddUserOtherUserExistsIsError(t *testing.T) {
	fx := &fakeXray{alterErr: errors.New("proxy/vless: User 11111111-2222-3333-4444-555555555555 already exists.")}
	c := startFake(t, fx, time.Second)
	if err := c.AddUser(context.Background(), "vless-in", testUUID, testUUID, ""); err == nil {
		t.Fatal("another user's conflict must stay an error")
	}
}

func TestAddUserUnknownInboundIsError(t *testing.T) {
	fx := &fakeXray{alterErr: errors.New("app/proxyman/command: failed to get handler: vless-in > app/proxyman/inbound: handler not found: vless-in")}
	c := startFake(t, fx, time.Second)
	err := c.AddUser(context.Background(), "vless-in", testUUID, testUUID, "")
	if err == nil || !strings.Contains(err.Error(), "handler not found") {
		t.Fatalf("err = %v, want handler not found", err)
	}
}

func TestAddUserUnavailableIsError(t *testing.T) {
	fx := &fakeXray{alterErr: status.Error(codes.Unavailable, "connection refused")}
	c := startFake(t, fx, time.Second)
	err := c.AddUser(context.Background(), "vless-in", testUUID, testUUID, "")
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("err = %v, want Unavailable", err)
	}
}

func TestAddUserRejectsInvalidUUIDWithoutCall(t *testing.T) {
	fx := &fakeXray{}
	c := startFake(t, fx, time.Second)
	for _, bad := range []string{"not-a-uuid", "", "6f1c2c1e3b1a4c559d5e0a1b2c3d4e5f", testUUID + "0", "6f1c2c1e-3b1a-4c55-9d5e-0a1b2c3d4e5g"} {
		err := c.AddUser(context.Background(), "vless-in", bad, bad, "")
		if err == nil {
			t.Fatalf("%q accepted", bad)
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Fatalf("error leaks the value: %v", err)
		}
	}
	if n := len(fx.alterCalls()); n != 0 {
		t.Fatalf("alter calls = %d, want 0", n)
	}
}

func TestAddUserAcceptsUpperCaseUUID(t *testing.T) {
	fx := &fakeXray{}
	c := startFake(t, fx, time.Second)
	if err := c.AddUser(context.Background(), "vless-in", strings.ToUpper(testUUID), strings.ToUpper(testUUID), ""); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveUserSendsEmail(t *testing.T) {
	fx := &fakeXray{}
	c := startFake(t, fx, time.Second)
	if err := c.RemoveUser(context.Background(), "vless-in", testUUID); err != nil {
		t.Fatal(err)
	}
	calls := fx.alterCalls()
	if len(calls) != 1 || calls[0].GetTag() != "vless-in" {
		t.Fatalf("calls = %v", calls)
	}
	if calls[0].GetOperation().GetType() != "xray.app.proxyman.command.RemoveUserOperation" {
		t.Fatalf("operation type = %q", calls[0].GetOperation().GetType())
	}
	var op proxymancmd.RemoveUserOperation
	if err := proto.Unmarshal(calls[0].GetOperation().GetValue(), &op); err != nil {
		t.Fatal(err)
	}
	if op.GetEmail() != testUUID {
		t.Fatalf("email = %q", op.GetEmail())
	}
}

func TestRemoveUserNotFoundIsSuccess(t *testing.T) {
	fx := &fakeXray{alterErr: errors.New("proxy/vless: User " + testUUID + " not found.")}
	c := startFake(t, fx, time.Second)
	if err := c.RemoveUser(context.Background(), "vless-in", testUUID); err != nil {
		t.Fatalf("not found must be success, got %v", err)
	}
}

func TestRemoveUserUnknownInboundIsError(t *testing.T) {
	fx := &fakeXray{alterErr: errors.New("app/proxyman/command: failed to get handler: vless-in > app/proxyman/inbound: handler not found: vless-in")}
	c := startFake(t, fx, time.Second)
	if err := c.RemoveUser(context.Background(), "vless-in", testUUID); err == nil {
		t.Fatal("missing inbound must not look like a removed user")
	}
}

func TestListUsersReturnsEmails(t *testing.T) {
	fx := &fakeXray{users: []*protocol.User{{Email: "a"}, {Email: testUUID}}}
	c := startFake(t, fx, time.Second)
	got, err := c.ListUsers(context.Background(), "vless-in")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != testUUID {
		t.Fatalf("users = %v", got)
	}
	lists := fx.listCalls()
	if len(lists) != 1 || lists[0].GetTag() != "vless-in" || lists[0].GetEmail() != "" {
		t.Fatalf("list request = %v", lists)
	}
}

func TestListUsersEmptyInbound(t *testing.T) {
	c := startFake(t, &fakeXray{}, time.Second)
	got, err := c.ListUsers(context.Background(), "vless-in")
	if err != nil || len(got) != 0 {
		t.Fatalf("users = %v, err = %v", got, err)
	}
}

func TestListUsersError(t *testing.T) {
	fx := &fakeXray{listErr: errors.New("app/proxyman/inbound: handler not found: vless-in")}
	c := startFake(t, fx, time.Second)
	if _, err := c.ListUsers(context.Background(), "vless-in"); err == nil {
		t.Fatal("want error")
	}
}

func TestQueryStatsMapsCounters(t *testing.T) {
	fx := &fakeXray{stats: []*statscmd.Stat{
		{Name: "user>>>" + testUUID + ">>>traffic>>>uplink", Value: 24},
		{Name: "user>>>" + testUUID + ">>>traffic>>>downlink", Value: 1000},
	}}
	c := startFake(t, fx, time.Second)
	got, err := c.QueryStats(context.Background(), "user>>>")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["user>>>"+testUUID+">>>traffic>>>uplink"] != 24 || got["user>>>"+testUUID+">>>traffic>>>downlink"] != 1000 {
		t.Fatalf("stats = %v", got)
	}
	q := fx.queryCalls()
	if len(q) != 1 || q[0].GetPattern() != "user>>>" || q[0].GetReset_() {
		t.Fatalf("query = %v, want pattern user>>> without reset", q)
	}
}

func TestQueryStatsEmpty(t *testing.T) {
	c := startFake(t, &fakeXray{}, time.Second)
	got, err := c.QueryStats(context.Background(), "user>>>")
	if err != nil || len(got) != 0 {
		t.Fatalf("stats = %v, err = %v", got, err)
	}
}

func TestQueryStatsError(t *testing.T) {
	fx := &fakeXray{statsErr: status.Error(codes.Unavailable, "down")}
	c := startFake(t, fx, time.Second)
	if _, err := c.QueryStats(context.Background(), "user>>>"); status.Code(err) != codes.Unavailable {
		t.Fatalf("err = %v, want Unavailable", err)
	}
}

func TestUptimeSeconds(t *testing.T) {
	c := startFake(t, &fakeXray{uptime: 4242}, time.Second)
	got, err := c.Uptime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != 4242*time.Second {
		t.Fatalf("uptime = %v", got)
	}
}

func TestUptimeError(t *testing.T) {
	fx := &fakeXray{sysErr: status.Error(codes.Unavailable, "down")}
	c := startFake(t, fx, time.Second)
	if _, err := c.Uptime(context.Background()); status.Code(err) != codes.Unavailable {
		t.Fatalf("err = %v, want Unavailable", err)
	}
}

func TestEveryCallHasTimeout(t *testing.T) {
	fx := &fakeXray{block: true}
	c := startFake(t, fx, 100*time.Millisecond)
	calls := map[string]func(context.Context) error{
		"add":    func(ctx context.Context) error { return c.AddUser(ctx, "vless-in", testUUID, testUUID, "") },
		"remove": func(ctx context.Context) error { return c.RemoveUser(ctx, "vless-in", testUUID) },
		"list": func(ctx context.Context) error {
			_, err := c.ListUsers(ctx, "vless-in")
			return err
		},
		"stats": func(ctx context.Context) error {
			_, err := c.QueryStats(ctx, "user>>>")
			return err
		},
		"uptime": func(ctx context.Context) error {
			_, err := c.Uptime(ctx)
			return err
		},
	}
	for name, call := range calls {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		start := time.Now()
		err := call(ctx)
		cancel()
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("%s: err = %v, want DeadlineExceeded", name, err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("%s: took %v, client timeout not applied", name, elapsed)
		}
	}
}

func TestDialRejectsNonPositiveTimeout(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		if _, err := Dial("127.0.0.1:10085", d); err == nil {
			t.Fatalf("timeout %v accepted", d)
		}
	}
}
