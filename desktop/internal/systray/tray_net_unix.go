//go:build !windows

// Unix/Linux/macOS 平台的 netListen 实现.
package systray

import "net"

func init() {
	netListen = func(network, addr string) (TCPListener, error) {
		l, err := net.Listen(network, addr)
		if err != nil {
			return nil, err
		}
		return &unixListener{l: l}, nil
	}
}

type unixListener struct {
	l net.Listener
}

func (u *unixListener) Addr() TCPAddr { return u.l.Addr() }
func (u *unixListener) Close() error  { return u.l.Close() }
