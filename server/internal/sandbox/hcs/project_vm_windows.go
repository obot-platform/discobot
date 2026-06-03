//go:build windows

package hcs

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/windows"
)

type projectVM struct {
	projectID  string
	vmID       windows.GUID
	proc       *launcherProcess
	consoleLog interface{ Close() error }
}

func (p *projectVM) ProjectID() string { return p.projectID }

func (p *projectVM) DockerDialer() func(context.Context, string, string) (net.Conn, error) {
	return p.PortDialer(dockerSockPort)
}

func (p *projectVM) PortDialer(port uint32) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialHVSock(ctx, p.vmID, serviceGUID(port))
	}
}

func (p *projectVM) WorkspaceMountSource(source string) (string, error) {
	return source, nil
}

func (p *projectVM) Shutdown() error {
	var firstErr error
	if p.proc != nil {
		if err := p.proc.stop(gracefulLauncherStopTimeout); err != nil {
			firstErr = err
		}
	}
	if p.consoleLog != nil {
		if err := p.consoleLog.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type hvAddr struct {
	vmID      windows.GUID
	serviceID windows.GUID
}

func (a hvAddr) Network() string { return "hvsock" }
func (a hvAddr) String() string {
	return fmt.Sprintf("%s/%s", formatGUID(a.vmID), formatGUID(a.serviceID))
}

type hvConn struct {
	socket    windows.Handle
	local     hvAddr
	remote    hvAddr
	closeOnce chan struct{}
}

func dialHVSock(ctx context.Context, vmID, serviceID windows.GUID) (net.Conn, error) {
	s, err := windows.Socket(afHyperV, sockStream, hvProtocolRaw)
	if err != nil {
		return nil, err
	}
	conn := &hvConn{socket: s, remote: hvAddr{vmID: vmID, serviceID: serviceID}, closeOnce: make(chan struct{})}

	done := make(chan error, 1)
	go func() {
		done <- connectHVSock(s, vmID, serviceID)
	}()

	select {
	case err := <-done:
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	case <-ctx.Done():
		_ = conn.Close()
		return nil, ctx.Err()
	}
}

func (c *hvConn) Read(b []byte) (int, error) {
	n, err := recvHVSock(c.socket, b)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, net.ErrClosed
	}
	return n, nil
}

func (c *hvConn) Write(b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := sendHVSock(c.socket, b[total:])
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, net.ErrClosed
		}
		total += n
	}
	return total, nil
}

func (c *hvConn) Close() error {
	select {
	case <-c.closeOnce:
		return nil
	default:
		close(c.closeOnce)
	}
	return windows.Closesocket(c.socket)
}

func (c *hvConn) LocalAddr() net.Addr                { return c.local }
func (c *hvConn) RemoteAddr() net.Addr               { return c.remote }
func (c *hvConn) SetDeadline(_ time.Time) error      { return nil }
func (c *hvConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *hvConn) SetWriteDeadline(_ time.Time) error { return nil }
