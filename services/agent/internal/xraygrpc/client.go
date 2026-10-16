package xraygrpc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/protocol"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/proxymancmd"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/routercmd"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/serial"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/statscmd"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/vless"
)

var errInvalidUserID = errors.New("invalid user id")

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Client struct {
	conn    *grpc.ClientConn
	handler proxymancmd.HandlerServiceClient
	stats   statscmd.StatsServiceClient
	routing routercmd.RoutingServiceClient
	timeout time.Duration
}

func Dial(addr string, timeout time.Duration) (*Client, error) {
	return dial(addr, timeout)
}

func dial(target string, timeout time.Duration, opts ...grpc.DialOption) (*Client, error) {
	if timeout <= 0 {
		return nil, errors.New("xray timeout must be positive")
	}
	opts = append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.Config{BaseDelay: 200 * time.Millisecond, Multiplier: 1.6, Jitter: 0.2, MaxDelay: 500 * time.Millisecond},
			MinConnectTimeout: timeout,
		}),
	}, opts...)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("xray dial: %w", err)
	}
	return &Client{
		conn:    conn,
		handler: proxymancmd.NewHandlerServiceClient(conn),
		stats:   statscmd.NewStatsServiceClient(conn),
		routing: routercmd.NewRoutingServiceClient(conn),
		timeout: timeout,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) AddUser(ctx context.Context, inboundTag string, uuid string, flow string) error {
	if !uuidPattern.MatchString(uuid) {
		return errInvalidUserID
	}
	account, err := typed(&vless.Account{Id: uuid, Flow: flow})
	if err != nil {
		return err
	}
	op, err := typed(&proxymancmd.AddUserOperation{User: &protocol.User{Email: uuid, Account: account}})
	if err != nil {
		return err
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	_, err = c.handler.AlterInbound(ctx, &proxymancmd.AlterInboundRequest{Tag: inboundTag, Operation: op})
	if err != nil && !isUserError(err, uuid, "already exists") {
		return fmt.Errorf("xray add user: %w", err)
	}
	return nil
}

func (c *Client) RemoveUser(ctx context.Context, inboundTag string, uuid string) error {
	op, err := typed(&proxymancmd.RemoveUserOperation{Email: uuid})
	if err != nil {
		return err
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	_, err = c.handler.AlterInbound(ctx, &proxymancmd.AlterInboundRequest{Tag: inboundTag, Operation: op})
	if err != nil && !isUserError(err, uuid, "not found") {
		return fmt.Errorf("xray remove user: %w", err)
	}
	return nil
}

func (c *Client) ListUsers(ctx context.Context, inboundTag string) ([]string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, err := c.handler.GetInboundUsers(ctx, &proxymancmd.GetInboundUserRequest{Tag: inboundTag})
	if err != nil {
		return nil, fmt.Errorf("xray list users: %w", err)
	}
	out := make([]string, 0, len(resp.GetUsers()))
	for _, u := range resp.GetUsers() {
		out = append(out, u.GetEmail())
	}
	return out, nil
}

func (c *Client) QueryStats(ctx context.Context, pattern string) (map[string]int64, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, err := c.stats.QueryStats(ctx, &statscmd.QueryStatsRequest{Pattern: pattern})
	if err != nil {
		return nil, fmt.Errorf("xray query stats: %w", err)
	}
	out := make(map[string]int64, len(resp.GetStat()))
	for _, s := range resp.GetStat() {
		out[s.GetName()] = s.GetValue()
	}
	return out, nil
}

func (c *Client) Uptime(ctx context.Context) (time.Duration, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, err := c.stats.GetSysStats(ctx, &statscmd.SysStatsRequest{})
	if err != nil {
		return 0, fmt.Errorf("xray sys stats: %w", err)
	}
	return time.Duration(resp.GetUptime()) * time.Second, nil
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}

func typed(m proto.Message) (*serial.TypedMessage, error) {
	b, err := proto.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("xray encode %s: %w", proto.MessageName(m), err)
	}
	return &serial.TypedMessage{Type: string(proto.MessageName(m)), Value: b}, nil
}

func isUserError(err error, email string, what string) bool {
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unknown {
		return false
	}
	return strings.Contains(strings.ToLower(st.Message()), strings.ToLower("User "+email+" "+what))
}
