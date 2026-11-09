package agent

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"syscall"
	"time"
)

const (
	sockDiagByFamily = 20
	sockDestroy      = 21
	diagMsgLen       = 72
	diagMsgIDAt      = 4
	diagRecvBuf      = 1 << 16
	tcpEstablished   = 1
	tcpFinWait1      = 4
	tcpFinWait2      = 5
	tcpCloseWait     = 8
	tcpListen        = 10
	killableStates   = 1<<tcpEstablished | 1<<tcpFinWait1 | 1<<tcpFinWait2 | 1<<tcpCloseWait
)

type diagID [48]byte

type netlinkSockets struct{}

func (netlinkSockets) Kill(ctx context.Context, ports map[uint16]bool, ips map[netip.Addr]bool) (int, error) {
	killed := 0
	var failed []error
	for _, family := range []uint8{syscall.AF_INET, syscall.AF_INET6} {
		n, err := killFamily(ctx, family, ports, ips)
		killed += n
		if err != nil {
			failed = append(failed, err)
		}
	}
	return killed, errors.Join(failed...)
}

func killFamily(ctx context.Context, family uint8, ports map[uint16]bool, ips map[netip.Addr]bool) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_INET_DIAG)
	if err != nil {
		return 0, fmt.Errorf("open sock_diag: %w", err)
	}
	defer func() { _ = syscall.Close(fd) }()
	if err := setDiagTimeouts(ctx, fd); err != nil {
		return 0, fmt.Errorf("sock_diag timeouts: %w", err)
	}
	ids, err := dumpSockets(ctx, fd, family, ports, ips)
	if err != nil {
		return 0, fmt.Errorf("dump sockets: %w", err)
	}
	killed := 0
	var firstErr error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return killed, err
		}
		err := destroySocket(fd, family, id)
		switch {
		case err == nil:
			killed++
		case errors.Is(err, syscall.ENOENT):
		case firstErr == nil:
			firstErr = fmt.Errorf("destroy socket: %w", err)
		}
	}
	return killed, firstErr
}

func setDiagTimeouts(ctx context.Context, fd int) error {
	left := sessionKillTimeout
	if deadline, ok := ctx.Deadline(); ok {
		left = max(time.Until(deadline), time.Millisecond)
	}
	tv := syscall.NsecToTimeval(left.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return err
	}
	return syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, &tv)
}

func dumpSockets(ctx context.Context, fd int, family uint8, ports map[uint16]bool, ips map[netip.Addr]bool) ([]diagID, error) {
	req := diagRequest(sockDiagByFamily, syscall.NLM_F_REQUEST|syscall.NLM_F_DUMP, family, killableStates, diagID{})
	if err := diagSend(fd, req); err != nil {
		return nil, err
	}
	buf := make([]byte, diagRecvBuf)
	var out []diagID
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		msgs, err := diagRecv(fd, buf)
		if err != nil {
			return nil, err
		}
		ids, done, err := killableSockets(msgs, ports, ips)
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
		if done {
			return out, nil
		}
	}
}

func destroySocket(fd int, family uint8, id diagID) error {
	if err := diagSend(fd, diagRequest(sockDestroy, syscall.NLM_F_REQUEST|syscall.NLM_F_ACK, family, 0, id)); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	msgs, err := diagRecv(fd, buf)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if m.Header.Type == syscall.NLMSG_ERROR {
			return diagError(m)
		}
	}
	return errors.New("sock_diag sent no acknowledgement")
}

func diagSend(fd int, req []byte) error {
	for {
		err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK})
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

func diagRecv(fd int, buf []byte) ([]syscall.NetlinkMessage, error) {
	for {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return syscall.ParseNetlinkMessage(buf[:n])
	}
}

func diagRequest(typ, flags uint16, family uint8, states uint32, id diagID) []byte {
	b := make([]byte, syscall.SizeofNlMsghdr+8+len(id))
	binary.NativeEndian.PutUint32(b[0:], uint32(len(b)))
	binary.NativeEndian.PutUint16(b[4:], typ)
	binary.NativeEndian.PutUint16(b[6:], flags)
	b[16], b[17] = family, syscall.IPPROTO_TCP
	binary.NativeEndian.PutUint32(b[20:], states)
	copy(b[24:], id[:])
	return b
}

func killableSockets(msgs []syscall.NetlinkMessage, ports map[uint16]bool, ips map[netip.Addr]bool) ([]diagID, bool, error) {
	var out []diagID
	for _, m := range msgs {
		switch m.Header.Type {
		case syscall.NLMSG_DONE:
			return out, true, nil
		case syscall.NLMSG_ERROR:
			return nil, false, diagError(m)
		}
		if len(m.Data) < diagMsgLen {
			continue
		}
		var id diagID
		copy(id[:], m.Data[diagMsgIDAt:])
		if ports[binary.BigEndian.Uint16(id[0:])] && ips[peerAddr(m.Data[0], id)] {
			out = append(out, id)
		}
	}
	return out, false, nil
}

func peerAddr(family uint8, id diagID) netip.Addr {
	if family == syscall.AF_INET {
		return netip.AddrFrom4([4]byte(id[20:24]))
	}
	return netip.AddrFrom16([16]byte(id[20:36])).Unmap()
}

func diagError(m syscall.NetlinkMessage) error {
	if len(m.Data) < 4 {
		return errors.New("short sock_diag error")
	}
	code := int32(binary.NativeEndian.Uint32(m.Data))
	if code == 0 {
		return nil
	}
	return syscall.Errno(-code)
}
