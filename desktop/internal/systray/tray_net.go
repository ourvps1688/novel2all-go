//go:build windows

// Windows 平台的 netListen 实现.
package systray

import "net"

func init() {
	netListen = func(network, addr string) (TCPListener, error) {
		l, err := net.Listen(network, addr)
		if err != nil {
			return nil, err
		}
		return &winListener{l: l}, nil
	}
}

type winListener struct {
	l net.Listener
}

func (w *winListener) Addr() TCPAddr { return w.l.Addr() }
func (w *winListener) Close() error  { return w.l.Close() }
