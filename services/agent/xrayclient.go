package agent

import (
	"context"
	"encoding/json"
)

type XrayClient interface {
	AddUser(ctx context.Context, inboundTag string, uuid string) error
	RemoveUser(ctx context.Context, inboundTag string, uuid string) error
	QueryStats(ctx context.Context, pattern string) (map[string]int64, error)
	AddInbound(ctx context.Context, configJSON string) error
	RemoveInbound(ctx context.Context, inboundTag string) error
}

func jsonUnmarshalString(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
