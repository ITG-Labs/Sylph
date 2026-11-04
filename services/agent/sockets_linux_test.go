package agent

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func diagMsg(family uint8, sport uint16, peer string) syscall.NetlinkMessage {
	data := make([]byte, diagMsgLen)
	data[0], data[1] = family, 1
	binary.BigEndian.PutUint16(data[diagMsgIDAt:], sport)
	binary.BigEndian.PutUint16(data[diagMsgIDAt+2:], 40000)
	addr := netip.MustParseAddr(peer)
	copy(data[diagMsgIDAt+20:], addr.AsSlice())
	binary.NativeEndian.PutUint32(data[diagMsgIDAt+40:], 0xdeadbeef)
	return syscall.NetlinkMessage{Header: syscall.NlMsghdr{Type: sockDiagByFamily}, Data: data}
}

func TestKillableSocketsFiltersByLocalPortAndPeer(t *testing.T) {
	ports := map[uint16]bool{443: true}
	ips := addrs("203.0.113.7", "2001:db8::1")
	msgs := []syscall.NetlinkMessage{
		diagMsg(syscall.AF_INET, 443, "203.0.113.7"),
		diagMsg(syscall.AF_INET, 8443, "203.0.113.7"),
		diagMsg(syscall.AF_INET, 443, "198.51.100.9"),
		{Header: syscall.NlMsghdr{Type: sockDiagByFamily}, Data: make([]byte, diagMsgLen-1)},
	}
	ids, done, err := killableSockets(msgs, ports, ips)
	if err != nil || done {
		t.Fatalf("done = %v, err = %v", done, err)
	}
	if len(ids) != 1 {
		t.Fatalf("ids = %d, want the single socket on the public port with the right peer", len(ids))
	}
	if got := binary.NativeEndian.Uint32(ids[0][40:]); got != 0xdeadbeef {
		t.Fatalf("cookie = %x, the id must be copied whole so the kernel finds the same socket", got)
	}
	for _, c := range []struct {
		name string
		msg  syscall.NetlinkMessage
		want int
	}{
		{"v4 client on a dual-stack listener", diagMsg(syscall.AF_INET6, 443, "::ffff:203.0.113.7"), 1},
		{"native v6", diagMsg(syscall.AF_INET6, 443, "2001:db8::1"), 1},
		{"other v6", diagMsg(syscall.AF_INET6, 443, "2001:db8::2"), 0},
		{"core connection on an ephemeral port", diagMsg(syscall.AF_INET, 51000, "203.0.113.7"), 0},
	} {
		ids, _, err := killableSockets([]syscall.NetlinkMessage{c.msg}, ports, ips)
		if err != nil || len(ids) != c.want {
			t.Fatalf("%s: ids = %d, err = %v, want %d", c.name, len(ids), err, c.want)
		}
	}
}

func TestKillableSocketsEndOfDump(t *testing.T) {
	_, done, err := killableSockets([]syscall.NetlinkMessage{{Header: syscall.NlMsghdr{Type: syscall.NLMSG_DONE}}}, nil, nil)
	if !done || err != nil {
		t.Fatalf("done = %v, err = %v", done, err)
	}
	failed := make([]byte, 4)
	binary.NativeEndian.PutUint32(failed, uint32(0xffffffff))
	_, _, err = killableSockets([]syscall.NetlinkMessage{{Header: syscall.NlMsghdr{Type: syscall.NLMSG_ERROR}, Data: failed}}, nil, nil)
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("err = %v, want EPERM from the kernel", err)
	}
}

func TestDiagRequestLayout(t *testing.T) {
	var id diagID
	id[0], id[47] = 0x01, 0x02
	b := diagRequest(sockDestroy, syscall.NLM_F_REQUEST|syscall.NLM_F_ACK, syscall.AF_INET6, 0x102, id)
	if len(b) != 16+56 {
		t.Fatalf("len = %d", len(b))
	}
	if got := binary.NativeEndian.Uint32(b[0:]); got != uint32(len(b)) {
		t.Fatalf("nlmsg_len = %d", got)
	}
	if got := binary.NativeEndian.Uint16(b[4:]); got != sockDestroy {
		t.Fatalf("nlmsg_type = %d", got)
	}
	if got := binary.NativeEndian.Uint16(b[6:]); got != syscall.NLM_F_REQUEST|syscall.NLM_F_ACK {
		t.Fatalf("nlmsg_flags = %d", got)
	}
	if b[16] != syscall.AF_INET6 || b[17] != syscall.IPPROTO_TCP {
		t.Fatalf("family = %d, protocol = %d", b[16], b[17])
	}
	if got := binary.NativeEndian.Uint32(b[20:]); got != 0x102 {
		t.Fatalf("states = %x", got)
	}
	if !reflect.DeepEqual(b[24:], id[:]) {
		t.Fatal("socket id is not where the kernel reads it")
	}
}

func TestKillableStatesSkipListeners(t *testing.T) {
	if killableStates&(1<<tcpListen) != 0 {
		t.Fatal("a listener must never be in the dump: killing it takes the node down")
	}
	if killableStates&(1<<tcpEstablished) == 0 {
		t.Fatal("established sockets are the whole point")
	}
}

func tcpPair(t *testing.T, ln net.Listener, local string) (client net.Conn, server net.Conn) {
	t.Helper()
	d := net.Dialer{Timeout: 2 * time.Second}
	if local != "" {
		d.LocalAddr = &net.TCPAddr{IP: net.ParseIP(local)}
	}
	addr := ln.Addr().(*net.TCPAddr)
	host := "127.0.0.1"
	if addr.IP.To4() == nil && !addr.IP.IsUnspecified() {
		host = addr.IP.String()
	}
	client, err := d.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(addr.Port)))
	if err != nil {
		t.Fatal(err)
	}
	server, err = ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

func stillTalks(t *testing.T, what string, client, server net.Conn) {
	t.Helper()
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	_ = server.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Write([]byte("x")); err != nil {
		t.Fatalf("%s: write: %v", what, err)
	}
	buf := make([]byte, 1)
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatalf("%s: read: %v", what, err)
	}
}

func isDead(t *testing.T, what string, c net.Conn) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatalf("%s: read succeeded, socket is alive", what)
	} else {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatalf("%s: socket stayed open: %v", what, err)
		}
	}
}

func TestNetlinkKillOnLiveSockets(t *testing.T) {
	for _, c := range []struct {
		name, network, listen, peer, other string
	}{
		{"ipv4 listener", "tcp4", "127.0.0.1:0", "127.0.0.1", "127.0.0.2"},
		{"dual-stack listener with a v4 client", "tcp", "[::]:0", "127.0.0.1", "127.0.0.2"},
		{"ipv6 listener", "tcp6", "[::1]:0", "::1", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			ln, err := net.Listen(c.network, c.listen)
			if err != nil {
				t.Skipf("no such listener here: %v", err)
			}
			defer func() { _ = ln.Close() }()
			otherPort, err := net.Listen(c.network, c.listen)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = otherPort.Close() }()
			victim, victimSrv := tcpPair(t, ln, "")
			keepC, keepS := tcpPair(t, otherPort, "")
			var nearC, nearS net.Conn
			if c.other != "" {
				nearC, nearS = tcpPair(t, ln, c.other)
			}
			port := uint16(ln.Addr().(*net.TCPAddr).Port)
			ctx, cancel := context.WithTimeout(context.Background(), sessionKillTimeout)
			defer cancel()
			killed, err := netlinkSockets{}.Kill(ctx, map[uint16]bool{port: true}, addrs(c.peer))
			if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EOPNOTSUPP) {
				t.Skipf("no CAP_NET_ADMIN or no INET_DIAG_DESTROY here: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			if killed != 1 {
				t.Fatalf("killed = %d, want 1", killed)
			}
			isDead(t, "client side of the victim", victim)
			isDead(t, "server side of the victim", victimSrv)
			stillTalks(t, "same address on another port", keepC, keepS)
			if nearC != nil {
				stillTalks(t, "another address on the same port", nearC, nearS)
			}
			if _, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err != nil {
				t.Fatalf("listener died: %v", err)
			}
		})
	}
}

func TestNetlinkKillStopsAtDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (netlinkSockets{}).Kill(ctx, map[uint16]bool{1: true}, addrs("203.0.113.7")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancelled context", err)
	}
}
