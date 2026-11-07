package multiplayer

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type availabilityAccept struct {
	conn net.Conn
	err  error
}

// Drive the real Serve loop and net.Pipe frame dispatcher without launching
// a server process or opening a listening port.
type availabilityListener struct {
	steps    chan availabilityAccept
	accepted chan time.Time
	started  chan struct{}
	closed   chan struct{}
	start    sync.Once
	close    sync.Once
}

func newAvailabilityListener() *availabilityListener {
	return &availabilityListener{
		steps: make(chan availabilityAccept, maxConnections+16), accepted: make(chan time.Time, maxConnections+16),
		started: make(chan struct{}), closed: make(chan struct{}),
	}
}

func (l *availabilityListener) Accept() (net.Conn, error) {
	l.start.Do(func() { close(l.started) })
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	case step := <-l.steps:
		l.accepted <- time.Now()
		return step.conn, step.err
	}
}

func (l *availabilityListener) Close() error   { l.close.Do(func() { close(l.closed) }); return nil }
func (l *availabilityListener) Addr() net.Addr { return &net.TCPAddr{} }

func availabilityServer(t *testing.T) (*Server, *availabilityListener, <-chan error) {
	t.Helper()
	s, err := NewServer(NewHub(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	l := newAvailabilityListener()
	done := make(chan error, 1)
	go func() { done <- s.Serve(l); close(done) }()
	t.Cleanup(func() {
		s.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("Serve did not exit after Close")
		}
	})
	return s, l, done
}

func availabilityState(t *testing.T, s *Server, total, unauthenticated int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		gotTotal, gotUnauthenticated := len(s.conns), s.unauthenticated
		s.mu.Unlock()
		if gotTotal == total && gotUnauthenticated == unauthenticated {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("connections total=%d pending=%d, want %d/%d", gotTotal, gotUnauthenticated, total, unauthenticated)
		}
		time.Sleep(time.Millisecond)
	}
}

func availabilityPeer(t *testing.T, l *availabilityListener) net.Conn {
	t.Helper()
	server, peer := net.Pipe()
	t.Cleanup(func() { peer.Close(); server.Close() })
	l.steps <- availabilityAccept{conn: server}
	return peer
}

func availabilityPing(t *testing.T, peer net.Conn) {
	t.Helper()
	_ = peer.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(peer, "Ping{\n}\n"); err != nil {
		t.Fatal(err)
	}
	method, _, err := readFrame(bufio.NewReader(peer))
	if err != nil || method != "Pong" {
		t.Fatalf("Ping failed: %s %v", method, err)
	}
}

func availabilityTemporaryError() error {
	return &net.OpError{Op: "accept", Net: "tcp", Err: &os.SyscallError{Syscall: "accept", Err: syscall.EMFILE}}
}

func TestBattleListenerTemporaryErrorsRecoverAndPermanentErrorsStop(t *testing.T) {
	l := newAvailabilityListener()
	var log bytes.Buffer
	s, err := NewServer(NewHub(), slog.New(slog.NewTextHandler(&log, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	temporary := availabilityTemporaryError()
	if !temporary.(net.Error).Temporary() {
		t.Fatal("fixture is not a temporary socket error on this platform")
	}
	for i := 0; i < 3; i++ {
		l.steps <- availabilityAccept{err: temporary}
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(l) }()
	peer := availabilityPeer(t, l)
	availabilityPing(t, peer)
	var previous time.Time
	for i := 0; i < 4; i++ {
		select {
		case now := <-l.accepted:
			if i > 0 && now.Sub(previous) < (acceptRetryMin<<(i-1)) {
				t.Fatal("temporary Accept error retried without backoff")
			}
			previous = now
		case <-time.After(time.Second):
			t.Fatal("listener did not recover")
		}
	}
	permanent := errors.New("permanent listener failure")
	l.steps <- availabilityAccept{err: permanent}
	select {
	case err := <-done:
		if !errors.Is(err, permanent) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("permanent Accept error was retried")
	}
	s.Close()
	if strings.Count(log.String(), "temporary accept failure") != 1 {
		t.Fatal("temporary errors flooded the log")
	}
	availabilityState(t, s, 0, 0)
}

func TestBattleListenerCloseInterruptsAcceptAndBackoff(t *testing.T) {
	for _, retrying := range []bool{false, true} {
		t.Run(strconv.FormatBool(retrying), func(t *testing.T) {
			s, l, done := availabilityServer(t)
			if retrying {
				// Reach the capped one-second delay, rather than only testing a
				// five-millisecond sleep that would finish on its own.
				for i := 0; i < 9; i++ {
					l.steps <- availabilityAccept{err: availabilityTemporaryError()}
				}
				for i := 0; i < 9; i++ {
					select {
					case <-l.accepted:
					case <-time.After(2 * time.Second):
						t.Fatal("backoff did not advance")
					}
				}
			} else {
				select {
				case <-l.started:
				case <-time.After(time.Second):
					t.Fatal("Accept did not start")
				}
			}
			started := time.Now()
			s.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Close did not wake Accept/backoff")
			}
			if time.Since(started) >= 500*time.Millisecond {
				t.Fatal("Close waited for the retry timer")
			}
		})
	}
}

func TestBattleListenerUnauthenticatedLimitReleasesSlots(t *testing.T) {
	s, l, _ := availabilityServer(t)
	peers := make([]net.Conn, maxUnauthenticated)
	for i := range peers {
		peers[i] = availabilityPeer(t, l)
	}
	availabilityState(t, s, maxUnauthenticated, maxUnauthenticated)
	excess := availabilityPeer(t, l)
	_ = excess.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := excess.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("excess connection was not closed: %v", err)
	}
	availabilityPing(t, peers[0])
	peers[1].Close()
	availabilityState(t, s, maxUnauthenticated-1, maxUnauthenticated-1)
	replacement := availabilityPeer(t, l)
	availabilityPing(t, replacement)
	availabilityState(t, s, maxUnauthenticated, maxUnauthenticated)
	s.Close()
	availabilityState(t, s, 0, 0)
}

func TestBattleListenerTotalLimitPreservesExistingConnections(t *testing.T) {
	s, l, _ := availabilityServer(t)
	existing := availabilityPeer(t, l)
	availabilityPing(t, existing)
	availabilityState(t, s, 1, 1)
	// Occupy the authenticated portion of the registry without constructing
	// thousands of irrelevant rooms/engines. One real peer keeps processing.
	fillers := make([]*clientConn, maxConnections-1)
	s.mu.Lock()
	for i := range fillers {
		fillers[i] = &clientConn{conn: &sessionTestConn{}}
		s.conns[fillers[i]] = struct{}{}
	}
	s.mu.Unlock()
	t.Cleanup(func() {
		s.mu.Lock()
		for _, c := range fillers {
			delete(s.conns, c)
		}
		s.mu.Unlock()
	})
	excess := availabilityPeer(t, l)
	_ = excess.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := excess.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("excess connection was not closed: %v", err)
	}
	availabilityPing(t, existing)
	s.mu.Lock()
	delete(s.conns, fillers[0])
	s.mu.Unlock()
	replacement := availabilityPeer(t, l)
	availabilityPing(t, replacement)
	availabilityState(t, s, maxConnections, 2)
}

func TestBattleUnauthenticatedDeadlineDoesNotSlideWithPing(t *testing.T) {
	s, err := NewServer(NewHub(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	left, peer := net.Pipe()
	defer peer.Close()
	// Shorten this connection's absolute deadline, without adding a production
	// timeout setting. Exercise the real reader, dispatcher and deferred cleanup.
	c := &clientConn{server: s, conn: left, authDeadline: time.Now().Add(150 * time.Millisecond)}
	s.conns[c] = struct{}{}
	s.unauthenticated = 1
	s.workers.Add(1)
	go func() { defer s.workers.Done(); c.serve() }()
	for i := 0; i < 3; i++ {
		availabilityPing(t, peer)
		time.Sleep(10 * time.Millisecond)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("Ping extended unauthenticated lifetime: %v", err)
	}
	availabilityState(t, s, 0, 0)
}

func availabilityMembers(t *testing.T) []Member {
	t.Helper()
	_, members := nextBattleFixture(t)
	for i := range members {
		members[i].Level, members[i].LeaderCardID, members[i].LeaderLevel, members[i].LeaderFame = 1, 1, 1, 1
		members[i].PartsIDs, members[i].DeckHonorIDs = make([]int, 7), make([]int, 4)
	}
	return members
}

func TestBattlePendingCredentialsBoundedPerUserAcrossCreateAndEnter(t *testing.T) {
	h := NewHub()
	members := availabilityMembers(t)
	h.rooms[1] = &room{
		RoomSnapshot: RoomSnapshot{RoomID: 1, State: RoomStateOpen, Members: members[:1]},
		reservations: make(map[int]roomReservation), connections: make(map[int]*clientConn), comebackTokens: make(map[int]string),
	}
	if _, err := h.Reserve(1, members[1].UserID, 2); err != nil {
		t.Fatal(err)
	}
	spec := RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: members[0]}
	other, err := h.IssueCreate(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Owner = members[1]
	var issued []Credential
	fixedExpiry := time.Now().Add(pendingLifetime)
	for i := 0; i < 2*maxPendingPerUser; i++ {
		var c Credential
		if i%2 == 0 {
			c, err = h.IssueCreate(spec)
		} else {
			c, err = h.IssueEnter(1, members[1])
		}
		if err != nil {
			t.Fatal(err)
		}
		// Reproduce coarse clock resolution: equal expiry must not evict a
		// newer retry while retaining an older credential.
		h.mu.Lock()
		pending := h.pending[c.AuthToken]
		pending.ExpiresAt = fixedExpiry
		h.pending[c.AuthToken] = pending
		h.mu.Unlock()
		issued = append(issued, c)
	}
	if len(h.pending) != maxPendingPerUser+1 {
		t.Fatalf("pending requests grew to %d", len(h.pending))
	}
	if _, ok := h.pending[other.AuthToken]; !ok {
		t.Fatal("another account's credential was removed")
	}
	for i, credential := range issued {
		p, exists := h.pending[credential.AuthToken]
		if exists != (i >= len(issued)-maxPendingPerUser) {
			t.Fatalf("did not retain the recent retry window: issued=%d kept=%v", i, exists)
		}
		if exists && (time.Until(p.ExpiresAt) < pendingLifetime-time.Second || time.Until(p.ExpiresAt) > pendingLifetime) {
			t.Fatal("credential expiry changed")
		}
	}
	latest := issued[len(issued)-1]
	s := &Server{hub: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c := &clientConn{server: s, conn: &sessionTestConn{}}
	if err := c.handleEnter(joinCSV(strconv.Itoa(members[1].UserID), "1", "", "2", latest.AuthToken, latest.Signature, "android")); err != nil {
		t.Fatal(err)
	}
	if c.userID != members[1].UserID || c.roomID != 1 {
		t.Fatal("latest join credential no longer works")
	}
	if _, ok := h.pending[latest.AuthToken]; ok {
		t.Fatal("credential was not consumed")
	}
}

func TestBattleRoomAuthenticationReturnsPendingSlot(t *testing.T) {
	s, l, _ := availabilityServer(t)
	members := availabilityMembers(t)
	credential, err := s.hub.IssueCreate(RoomSpec{BossID: 1, EnemyPartyID: 1, HoldMax: 5, Owner: members[0]})
	if err != nil {
		t.Fatal(err)
	}
	peer := availabilityPeer(t, l)
	_ = peer.SetDeadline(time.Now().Add(time.Second))
	payload := joinCSV(strconv.Itoa(members[0].UserID), "1", "0", "", "1", "1", "0", "0", "0", credential.AuthToken, credential.Signature, "android")
	if _, err := io.WriteString(peer, "RoomCreateRequest{\n"+payload+"\n}\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(peer)
	method, response, err := readFrame(reader)
	if err != nil || method != "RoomCreateRequestResult" || strings.HasPrefix(response, "-") {
		t.Fatalf("create rejected: %s %q %v", method, response, err)
	}
	// Drain the initial room members/vacancies so handleCreate can finish all
	// its writes; the pending slot is released at the outer dispatch boundary.
	for i := 0; i < 4; i++ {
		if _, _, err := readFrame(reader); err != nil {
			t.Fatal(err)
		}
	}
	availabilityState(t, s, 1, 0)
	availabilityPing(t, peer)
	peer.Close()
	availabilityState(t, s, 0, 0)
}
