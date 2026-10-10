package agent

import (
	"context"
	"encoding/json"
)

type StatsValue struct {
	Up   int64
	Down int64
}

type XrayClient interface {
	AddUser(ctx context.Context, inboundTag string, uuid string) error
	RemoveUser(ctx context.Context, inboundTag string, uuid string) error
	QueryStats(ctx context.Context, pattern string) (map[string]StatsValue, error)
	AddInbound(ctx context.Context, configJSON string) error
	RemoveInbound(ctx context.Context, inboundTag string) error
}

func jsonUnmarshalString(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
