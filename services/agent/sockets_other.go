//go:build !linux

package agent

import (
	"context"
	"errors"
	"net/netip"
)

type netlinkSockets struct{}

func (netlinkSockets) Kill(context.Context, map[uint16]bool, map[netip.Addr]bool) (int, error) {
	return 0, errors.New("destroying sockets needs linux")
}
