package netstats

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type partialConn struct{ net.Conn }

func (partialConn) Read(p []byte) (int, error)  { return copy(p, "hello"), io.EOF }
func (partialConn) Write(p []byte) (int, error) { return min(len(p), 3), io.ErrShortWrite }

type readerFromConn struct {
	partialConn
	called bool
}

func (c *readerFromConn) ReadFrom(io.Reader) (int64, error) {
	c.called = true
	return 7, io.ErrUnexpectedEOF
}

func TestCountsActualPartialIOAndPreservesReadFrom(t *testing.T) {
	var counters Counters
	conn := &countingConn{Conn: partialConn{}, traffic: &counters.BattleTCP}
	if n, err := conn.Read(make([]byte, 16)); n != 5 || err != io.EOF {
		t.Fatal("read contract changed", n, err)
	}
	if n, err := conn.Write([]byte("longer")); n != 3 || err != io.ErrShortWrite {
		t.Fatal("write contract changed", n, err)
	}
	if n, err := conn.ReadFrom(strings.NewReader("fallback")); n != 3 || err != io.ErrShortWrite {
		t.Fatal("fallback ReadFrom contract changed", n, err)
	}
	underlying := &readerFromConn{}
	conn.Conn = underlying
	if n, err := conn.ReadFrom(strings.NewReader("delegated")); n != 7 || err != io.ErrUnexpectedEOF || !underlying.called {
		t.Fatal("underlying ReadFrom not preserved", n, err)
	}
	snapshot := counters.Snapshot()
	if snapshot.BattleTCP.ReceivedBytesTotal != 5 || snapshot.BattleTCP.SentBytesTotal != 13 || snapshot.HTTP != (Bytes{}) || snapshot.Admin != (Bytes{}) {
		t.Fatal("partial bytes lost, double counted or crossed services", snapshot)
	}
}

func TestHTTPListenerCountsHeadersAndStreamedResponse(t *testing.T) {
	var counters Counters
	body := strings.Repeat("stream", 4096)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.Copy(w, io.LimitReader(strings.NewReader(body), int64(len(body))))
	}))
	server.Listener = counters.HTTP.WrapListener(server.Listener)
	server.Start()
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Close = true
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(got) != body {
		t.Fatal("streamed response changed", err)
	}
	server.Close()
	snapshot := counters.Snapshot()
	if snapshot.HTTP.ReceivedBytesTotal == 0 || snapshot.HTTP.SentBytesTotal <= uint64(len(body)) || snapshot.BattleTCP != (Bytes{}) {
		t.Fatal("listener did not count wire headers/body independently", snapshot)
	}
}

func TestHTTP2WorksThroughCountingListener(t *testing.T) {
	var counters Counters
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("HTTP2 request fell back to HTTP1")
		}
		_, _ = io.WriteString(w, "counted h2c response")
	}))
	server.Config.Protocols = protocols
	server.Listener = counters.HTTP.WrapListener(server.Listener)
	server.Start()
	defer server.Close()
	transport := &http.Transport{Protocols: protocols}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.ProtoMajor != 2 || string(body) != "counted h2c response" {
		t.Fatal("HTTP2 framing changed through the metrics listener", err)
	}
	stats := counters.Snapshot()
	if stats.HTTP.ReceivedBytesTotal == 0 || stats.HTTP.SentBytesTotal <= uint64(len(body)) {
		t.Fatal("HTTP2 wire bytes were not counted", stats)
	}
}
