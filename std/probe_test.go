// No trouble

package std

import (
	"net"
	"testing"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

// newFECSessionPair dials a real KCP pair over loopback with FEC enabled, which
// is what makes the out-of-band channel available.
func newFECSessionPair(t *testing.T) (client, server *kcp.UDPSession) {
	t.Helper()

	ln, err := kcp.ListenWithOptions("127.0.0.1:0", nil, 10, 3)
	if err != nil {
		t.Fatal(err)
	}

	accepted := make(chan *kcp.UDPSession, 1)
	go func() {
		conn, err := ln.AcceptKCP()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- conn
	}()

	client, err = kcp.DialWithOptions(ln.Addr().String(), nil, 10, 3)
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}

	// a listener only creates a session once a packet arrives, and a fresh KCP
	// client sends nothing on its own, so push one byte to introduce the pair
	if _, err := client.Write([]byte("introduce")); err != nil {
		client.Close()
		ln.Close()
		t.Fatal(err)
	}

	select {
	case server = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("server session was never accepted")
	}
	if server == nil {
		client.Close()
		ln.Close()
		t.Fatal("server session was not accepted")
	}

	t.Cleanup(func() {
		client.Close()
		server.Close()
		ln.Close()
	})
	return client, server
}

func TestHealthProbeIsAnswered(t *testing.T) {
	client, server := newFECSessionPair(t)

	mux, err := NewOOBMux(server)
	if err != nil {
		t.Fatal(err)
	}
	RegisterHealthResponder(mux)

	h := NewConnHealth(client)
	if h.mux == nil {
		t.Fatal("expected an out-of-band channel on a session with FEC")
	}

	// the path is idle, so only the probe can tell it apart from a blocked port
	if res := h.Probe(2 * time.Second); res != ProbeAlive {
		t.Fatalf("probe returned %v, expected %v", res, ProbeAlive)
	}
	if !h.PeerAnswersProbes() {
		t.Fatal("a reply should mark the peer as probe capable")
	}

	// a confirmed probe buys one more window of silence, and a session that dies
	// right afterwards is still caught when that window runs out
	h.MarkAlive()
	if h.SilentFor() > time.Second {
		t.Fatalf("an answered probe should refresh the clock, got %v", h.SilentFor())
	}
}

func TestHealthProbeTimesOutWithoutResponder(t *testing.T) {
	client, _ := newFECSessionPair(t)

	// the peer never installed a responder, an old server looks the same way
	h := NewConnHealth(client)
	if res := h.Probe(time.Second); res != ProbeDead {
		t.Fatalf("probe returned %v, expected %v", res, ProbeDead)
	}
	if h.PeerAnswersProbes() {
		t.Fatal("nothing ever answered, the peer must stay marked as incapable")
	}
}

func TestOOBMuxRoutesByMagic(t *testing.T) {
	mux := &OOBMux{}

	var pinged, ponged, probed int
	mux.Register(HealthPingMagic, func([]byte) { pinged++ })
	mux.Register(HealthPongMagic, func([]byte) { ponged++ })
	mux.Register(LossTestMagic, func([]byte) { probed++ })

	buf := make([]byte, HealthPktSize)
	mux.dispatch(buf[:EncodeHealthPing(buf, 1, time.Now())])
	mux.dispatch(buf[:0]) // empty payload must not panic on the prefix check

	c, _ := DecodeHealthPing(buf)
	if c.Nonce != 1 {
		t.Fatalf("nonce round trip broken: %+v", c)
	}
	EncodeHealthPong(buf, c)
	mux.dispatch(buf)

	// traffic of another feature lands on its own route and nowhere else
	mux.dispatch([]byte(LossTestMagic))
	mux.dispatch([]byte("something else"))

	if pinged != 1 || ponged != 1 || probed != 1 {
		t.Fatalf("ping %d pong %d loss test %d, expected one of each", pinged, ponged, probed)
	}
	if _, ok := DecodeHealthPong([]byte(HealthPingMagic)); ok {
		t.Fatal("a ping must not decode as a pong")
	}
}

// newTestHealth builds a tracker without a session, for the counters alone.
func newTestHealth() *ConnHealth {
	h := &ConnHealth{echoes: make(chan uint64, 8)}
	h.lastAlive.Store(time.Now().UnixNano())
	return h
}

func TestHealthStreamCountsTraffic(t *testing.T) {
	h := newTestHealth()
	inner, peer := net.Pipe()
	stream := NewHealthStream(inner, h)
	defer stream.Close()

	// net.Pipe is synchronous, so the far end has to drain what the wrapper writes
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64)
		for {
			if _, err := peer.Read(buf); err != nil {
				return
			}
		}
	}()

	// silence grows from the moment the session is built, nothing has arrived yet
	time.Sleep(20 * time.Millisecond)
	if h.SilentFor() < 10*time.Millisecond {
		t.Fatalf("expected a growing silence, got %v", h.SilentFor())
	}

	// net.Pipe is synchronous: inbound traffic has to arrive while this
	// goroutine is about to read it, not before
	go func() {
		peer.Write([]byte("heartbeat"))
	}()
	buf := make([]byte, 16)
	n, err := stream.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("read: %d %v", n, err)
	}
	if h.SilentFor() > time.Second {
		t.Fatalf("a read should reset the silence, got %v", h.SilentFor())
	}

	// a healthy write is counted as such, and resets nothing on the inbound side
	if _, err := stream.Write([]byte("filler")); err != nil {
		t.Fatal(err)
	}
	if h.WriteErrors() != 0 {
		t.Fatalf("unexpected write errors: %d", h.WriteErrors())
	}

	// a write that fails is what a transmit buffer which never drains looks like
	// from the outside, it has to leave a trace in the counters
	peer.Close()
	<-done
	if _, err := stream.Write([]byte("after close")); err == nil {
		t.Fatal("expected the write to fail once the peer is gone")
	}
	if h.WriteErrors() == 0 {
		t.Fatal("expected the failed write to be counted")
	}
	if h.String() == "" {
		t.Fatal("the summary is used for logging")
	}
}

func TestHealthStreamKeepsScatterGather(t *testing.T) {
	h := newTestHealth()
	inner, peer := net.Pipe()
	defer inner.Close()
	defer peer.Close()

	// a plain net.Conn has no WriteBuffers, so the wrapper must not advertise it,
	// otherwise smux would take a path the wrapped connection cannot serve
	if _, ok := NewHealthStream(inner, h).(writeBuffersConn); ok {
		t.Fatal("wrapper around a plain conn must not claim WriteBuffers")
	}

	// a connection which has it keeps it, so smux does not lose its zero copy path
	fake := &gatherConn{}
	if _, ok := NewHealthStream(fake, h).(writeBuffersConn); !ok {
		t.Fatal("wrapper around a gather writer must keep WriteBuffers")
	}
}

// gatherConn is a net.Conn with the scatter-gather write of kcp.UDPSession.
type gatherConn struct {
	net.Conn
	written int
}

func (c *gatherConn) Read([]byte) (int, error)         { return 0, net.ErrClosed }
func (c *gatherConn) Write(b []byte) (int, error)      { return len(b), nil }
func (c *gatherConn) Close() error                     { return nil }
func (c *gatherConn) LocalAddr() net.Addr              { return nil }
func (c *gatherConn) RemoteAddr() net.Addr             { return nil }
func (c *gatherConn) SetDeadline(time.Time) error      { return nil }
func (c *gatherConn) SetReadDeadline(time.Time) error  { return nil }
func (c *gatherConn) SetWriteDeadline(time.Time) error { return nil }
func (c *gatherConn) WriteBuffers(v [][]byte) (int, error) {
	n := 0
	for _, b := range v {
		n += len(b)
	}
	return n, nil
}
