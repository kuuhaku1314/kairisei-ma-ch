package main

import "net"

// Filter new BattleSv connections before handing them to the battle service.
type peerFilteredListener struct {
	net.Listener
	blocked func(string) bool
}

func (listener peerFilteredListener) Accept() (net.Conn, error) {
	for {
		conn, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if !listener.blocked(conn.RemoteAddr().String()) {
			return conn, nil
		}
		_ = conn.Close()
	}
}
