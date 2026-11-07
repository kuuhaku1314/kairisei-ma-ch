// Package netstats counts server socket bytes without inspecting payloads.
package netstats

import (
	"io"
	"net"
	"sync/atomic"
)

type Counters struct {
	HTTP      Traffic
	BattleTCP Traffic
	Admin     Traffic
}

type Snapshot struct {
	HTTP      Bytes `json:"http"`
	BattleTCP Bytes `json:"battle_tcp"`
	Admin     Bytes `json:"admin"`
}

type Bytes struct {
	ReceivedBytesTotal uint64 `json:"received_bytes_total"`
	SentBytesTotal     uint64 `json:"sent_bytes_total"`
}

func (c *Counters) Snapshot() Snapshot {
	return Snapshot{HTTP: c.HTTP.snapshot(), BattleTCP: c.BattleTCP.snapshot(), Admin: c.Admin.snapshot()}
}

type Traffic struct {
	received atomic.Uint64
	sent     atomic.Uint64
}

func (t *Traffic) snapshot() Bytes {
	return Bytes{ReceivedBytesTotal: t.received.Load(), SentBytesTotal: t.sent.Load()}
}

// WrapListener counts bytes actually read/written, including HTTP headers and
// compressed battle frames. IP/TCP headers, retransmits and external CDN traffic
// are outside this application's sockets and therefore outside these counters.
func (t *Traffic) WrapListener(listener net.Listener) net.Listener {
	return &countingListener{Listener: listener, traffic: t}
}

type countingListener struct {
	net.Listener
	traffic *Traffic
}

func (l *countingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: conn, traffic: l.traffic}, nil
}

type countingConn struct {
	net.Conn
	traffic *Traffic
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.traffic.received.Add(uint64(n))
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.traffic.sent.Add(uint64(n))
	return n, err
}

// Preserve TCPConn.ReadFrom (including sendfile for large resource responses).
// Count its actual result once; the fallback deliberately bypasses our Write.
func (c *countingConn) ReadFrom(r io.Reader) (int64, error) {
	var n int64
	var err error
	if readerFrom, ok := c.Conn.(io.ReaderFrom); ok {
		n, err = readerFrom.ReadFrom(r)
	} else {
		n, err = io.Copy(struct{ io.Writer }{c.Conn}, r)
	}
	c.traffic.sent.Add(uint64(n))
	return n, err
}
