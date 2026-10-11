package xraygrpc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/protocol"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/proxymancmd"
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
	flow    string
	timeout time.Duration
}

func Dial(addr string, flow string, timeout time.Duration) (*Client, error) {
	return dial(addr, flow, timeout)
}

func dial(target string, flow string, timeout time.Duration, opts ...grpc.DialOption) (*Client, error) {
	opts = append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, opts...)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("xray dial: %w", err)
	}
	return &Client{
		conn:    conn,
		handler: proxymancmd.NewHandlerServiceClient(conn),
		stats:   statscmd.NewStatsServiceClient(conn),
		flow:    flow,
		timeout: timeout,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) AddUser(ctx context.Context, inboundTag string, uuid string) error {
	if !uuidPattern.MatchString(uuid) {
		return errInvalidUserID
	}
	account, err := typed(&vless.Account{Id: uuid, Flow: c.flow})
	if err != nil {
		return err
	}
	op, err := typed(&proxymancmd.AddUserOperation{User: &protocol.User{Email: uuid, Account: account}})
	if err != nil {
		return err
	}
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
	_, err = c.handler.AlterInbound(ctx, &proxymancmd.AlterInboundRequest{Tag: inboundTag, Operation: op})
	if err != nil && !isUserError(err, uuid, "not found") {
		return fmt.Errorf("xray remove user: %w", err)
	}
	return nil
}

func (c *Client) ListUsers(ctx context.Context, inboundTag string) ([]string, error) {
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
