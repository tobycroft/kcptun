// No trouble

package std

import (
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

// ConnHealth is the observable liveness of a single multiplexed session.
//
// smux sends its NOP heartbeats on its own and throws the result away, so a
// client cannot ask the session whether the port underneath it still works.
// This type collects what is visible from outside instead: every byte read from
// the peer stamps a deadline, and a liveness probe over KCP out-of-band packets
// decides whether a silent path is really blocked.
type ConnHealth struct {
	// lastAlive is the last sign of life: a byte read from the peer or a probe
	// the peer answered. Stamping it from a successful probe is what keeps an
	// idle connection out of trouble without hiding one that dies afterwards.
	lastAlive   atomic.Int64 // unix nano
	writeErrors atomic.Int64
	readErrors  atomic.Int64
	pongs       atomic.Uint64 // pongs received since the session was created
	nonce       atomic.Uint64 // ping sequence, matched by the pong handler
	peerAnswers atomic.Bool   // the far end replies to probes

	mux    *OOBMux
	echoes chan uint64 // nonces answered by a pong
}

// NewConnHealth builds the health tracker of a session together with its
// out-of-band dispatcher. OOB requires FEC, so on a session without FEC the
// tracker still counts traffic and only the probe degrades to ProbeUnavailable.
func NewConnHealth(conn *kcp.UDPSession) *ConnHealth {
	h := &ConnHealth{
		echoes: make(chan uint64, 8),
	}
	// silence is measured from the birth of the session, a port that is already
	// blocked when it is dialed still gets judged once the window runs out
	h.lastAlive.Store(time.Now().UnixNano())

	mux, err := NewOOBMux(conn)
	if err != nil {
		// no out-of-band channel, the silence detector still works
		return h
	}
	h.mux = mux
	mux.Register(HealthPongMagic, func(b []byte) {
		p, ok := DecodeHealthPong(b)
		if !ok {
			return
		}
		h.pongs.Add(1)
		h.peerAnswers.Store(true)
		select {
		case h.echoes <- p.Nonce:
		default:
			// drop the oldest echo, only the newest one matters
			select {
			case <-h.echoes:
			default:
			}
			select {
			case h.echoes <- p.Nonce:
			default:
			}
		}
	})
	return h
}

// writeBuffersConn is the scatter-gather writer smux looks for.
type writeBuffersConn interface {
	net.Conn
	WriteBuffers(v [][]byte) (int, error)
}

// NewHealthStream wraps the connection handed over to smux and feeds inbound
// activity into the tracker.
//
// The wrapper only advertises WriteBuffers when the inner connection really
// supports it: smux type-asserts that method to choose its zero copy path, so a
// wrapper claiming it unconditionally would change how every frame is packetized.
func NewHealthStream(inner net.Conn, h *ConnHealth) net.Conn {
	if _, ok := inner.(writeBuffersConn); ok {
		return &healthBuffersStream{healthConn{Conn: inner, h: h}}
	}
	return &healthConn{Conn: inner, h: h}
}

type healthConn struct {
	net.Conn
	h *ConnHealth
}

// healthBuffersStream is the same wrapper with the scatter-gather write exposed.
type healthBuffersStream struct {
	healthConn
}

var (
	_ net.Conn         = (*healthConn)(nil)
	_ net.Conn         = (*healthBuffersStream)(nil)
	_ writeBuffersConn = (*healthBuffersStream)(nil)
)

func (c *healthConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.h.touchInbound()
	} else if err != nil && err != io.EOF {
		c.h.readErrors.Add(1)
	}
	return n, err
}

func (c *healthConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if err != nil {
		c.h.writeErrors.Add(1)
	}
	return n, err
}

// WriteBuffers keeps the zero copy path of smux available when the underlying
// connection offers it.
func (c *healthBuffersStream) WriteBuffers(v [][]byte) (int, error) {
	n, err := c.Conn.(writeBuffersConn).WriteBuffers(v)
	if err != nil {
		c.h.writeErrors.Add(1)
	}
	return n, err
}

func (h *ConnHealth) touchInbound() {
	h.lastAlive.Store(time.Now().UnixNano())
}

// SilentFor reports how long the session has shown no sign of life, counted from
// the last byte read and from the last probe the peer answered, whichever came
// later.
func (h *ConnHealth) SilentFor() time.Duration {
	return time.Since(time.Unix(0, h.lastAlive.Load()))
}

// WriteErrors counts failed frame writes, a growing counter means the transmit
// buffer never drains, which is what a blocked port looks like from inside.
func (h *ConnHealth) WriteErrors() int64 {
	return h.writeErrors.Load()
}

// PeerAnswersProbes reports whether the far end ever replied to a probe. It
// tells a peer running an older build apart from a path that is really gone.
func (h *ConnHealth) PeerAnswersProbes() bool {
	return h.peerAnswers.Load()
}

// MarkAlive records a probe that proved liveness. It stamps the same clock a
// read would, so an idle session is looked at again after one more window
// instead of being skipped forever, and a session that dies right after a good
// probe is still caught one window later.
func (h *ConnHealth) MarkAlive() {
	h.lastAlive.Store(time.Now().UnixNano())
}

// String summarizes the counters for logging.
func (h *ConnHealth) String() string {
	return fmt.Sprintf("silent for %v, write errors %d, read errors %d, pongs %d, probe supported %v",
		h.SilentFor().Truncate(time.Second), h.writeErrors.Load(), h.readErrors.Load(), h.pongs.Load(), h.mux != nil)
}

// ProbeResult is the outcome of a liveness probe.
type ProbeResult int

const (
	// ProbeAlive means the peer answered, the path works although it is idle.
	ProbeAlive ProbeResult = iota
	// ProbeDead means pings went out and nothing came back.
	ProbeDead
	// ProbeUnavailable means this session cannot probe, OOB requires FEC.
	ProbeUnavailable
)

func (r ProbeResult) String() string {
	switch r {
	case ProbeAlive:
		return "alive"
	case ProbeDead:
		return "dead"
	default:
		return "unavailable"
	}
}

// probeRetry is how often a single probe re-sends its ping, because OOB packets
// are neither retransmitted nor acknowledged.
const probeRetry = 3

// Probe asks the peer whether the path is alive, re-sending the ping through
// timeout so one lost OOB packet cannot fake a blocked port.
func (h *ConnHealth) Probe(timeout time.Duration) ProbeResult {
	if h.mux == nil || timeout <= 0 {
		return ProbeUnavailable
	}

	// drain stale echoes of earlier rounds
	for {
		select {
		case <-h.echoes:
			continue
		default:
		}
		break
	}

	nonce := h.nonce.Add(1)
	buf := make([]byte, HealthPktSize)
	EncodeHealthPing(buf, nonce, time.Now())
	if err := h.mux.SendOOB(buf); err != nil {
		return ProbeUnavailable
	}

	retry := timeout / probeRetry
	if retry <= 0 {
		retry = timeout
	}
	deadline := time.After(timeout)
	ticker := time.NewTicker(retry)
	defer ticker.Stop()
	for {
		select {
		case got := <-h.echoes:
			if got == nonce {
				return ProbeAlive
			}
		case <-ticker.C:
			EncodeHealthPing(buf, nonce, time.Now())
			if err := h.mux.SendOOB(buf); err != nil {
				return ProbeDead
			}
		case <-deadline:
			return ProbeDead
		}
	}
}
