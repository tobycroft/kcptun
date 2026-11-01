// No trouble

package std

import (
	"bytes"
	"encoding/binary"
	"sync"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

// ---------------------------------------------------------------------------
// Out-of-band dispatcher
// ---------------------------------------------------------------------------

// OOBMux fans a single KCP out-of-band callback out to several features.
//
// kcp-go keeps exactly one OOB callback per session (see SetOOBHandler), so the
// loss test and the health probe cannot both own it. Every payload is matched
// against the registered magics, the first prefix wins.
//
// The callback runs synchronously inside the packet input path of the session,
// therefore a registered handler MUST return quickly and MUST NOT block, or it
// stalls every other packet of that session.
type OOBMux struct {
	conn     *kcp.UDPSession
	mu       sync.RWMutex
	handlers []oobRoute
}

type oobRoute struct {
	magic   []byte
	handler func([]byte)
}

// NewOOBMux installs the dispatcher on a session and returns it. OOB requires
// FEC, so an error is returned when the session has no FEC encoder; callers
// then run without out-of-band features.
func NewOOBMux(conn *kcp.UDPSession) (*OOBMux, error) {
	m := &OOBMux{conn: conn}
	if err := conn.SetOOBHandler(m.dispatch); err != nil {
		return nil, err
	}
	return m, nil
}

// Register binds a handler to payloads starting with magic. Shorter magics
// should be registered first when several magics share a prefix.
func (m *OOBMux) Register(magic string, handler func([]byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, oobRoute{magic: []byte(magic), handler: handler})
}

// SendOOB queues an out-of-band packet. OOB is unreliable by design: the packet
// is neither retransmitted nor acknowledged, and it is dropped silently when
// the internal queue is full.
func (m *OOBMux) SendOOB(b []byte) error {
	return m.conn.SendOOB(b)
}

// MaxSize returns the largest OOB payload the session can carry, 0 when OOB is
// unavailable.
func (m *OOBMux) MaxSize() int {
	return m.conn.GetOOBMaxSize()
}

func (m *OOBMux) dispatch(b []byte) {
	m.mu.RLock()
	routes := m.handlers
	m.mu.RUnlock()

	for i := range routes {
		if bytes.HasPrefix(b, routes[i].magic) {
			routes[i].handler(b)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Health probe protocol, carried in KCP OOB packets
// ---------------------------------------------------------------------------

const (
	// HealthPingMagic marks an OOB payload as a liveness ping.
	HealthPingMagic = "KCPHPNG1"
	// HealthPongMagic marks an OOB payload as the reply to a liveness ping.
	HealthPongMagic = "KCPHPOG1"
	// HealthPktSize is magic + nonce + send timestamp.
	HealthPktSize = 24
)

// healthProbeMagic / healthPongMagic are the byte forms used for matching.
var (
	healthProbeMagic = []byte(HealthPingMagic)
	healthPongMagic  = []byte(HealthPongMagic)
)

// HealthPing is a decoded liveness ping. The nonce mirrors the value that the
// pong has to carry back, the timestamp lets the sender measure the round trip.
type HealthPing struct {
	Nonce uint64
	Sent  time.Time
}

// EncodeHealthPing fills buf with a ping and returns the number of bytes used.
// buf must be at least HealthPktSize bytes.
func EncodeHealthPing(buf []byte, nonce uint64, sent time.Time) int {
	if len(buf) < HealthPktSize {
		return 0
	}
	copy(buf[:len(healthProbeMagic)], healthProbeMagic)
	binary.LittleEndian.PutUint64(buf[8:], nonce)
	binary.LittleEndian.PutUint64(buf[16:], uint64(sent.UnixNano()))
	return HealthPktSize
}

// EncodeHealthPong fills buf with the reply echoing a ping.
func EncodeHealthPong(buf []byte, p HealthPing) int {
	if len(buf) < HealthPktSize {
		return 0
	}
	copy(buf[:len(healthPongMagic)], healthPongMagic)
	binary.LittleEndian.PutUint64(buf[8:], p.Nonce)
	binary.LittleEndian.PutUint64(buf[16:], uint64(p.Sent.UnixNano()))
	return HealthPktSize
}

// decodeHealth parses a ping or a pong of the requested kind.
func decodeHealth(b []byte, magic []byte) (p HealthPing, ok bool) {
	if len(b) < HealthPktSize || !bytes.Equal(b[:len(magic)], magic) {
		return p, false
	}
	p.Nonce = binary.LittleEndian.Uint64(b[8:])
	p.Sent = time.Unix(0, int64(binary.LittleEndian.Uint64(b[16:])))
	return p, true
}

// DecodeHealthPing parses a ping payload, ok is false for other traffic.
func DecodeHealthPing(b []byte) (HealthPing, bool) {
	return decodeHealth(b, healthProbeMagic)
}

// DecodeHealthPong parses a pong payload, ok is false for other traffic.
func DecodeHealthPong(b []byte) (HealthPing, bool) {
	return decodeHealth(b, healthPongMagic)
}

// RegisterHealthResponder makes a session answer every liveness ping with the
// matching pong. It is stateless, so repeated or reordered pings are harmless
// and a restart of either end never strands counters.
func RegisterHealthResponder(mux *OOBMux) {
	mux.Register(HealthPingMagic, func(b []byte) {
		p, ok := DecodeHealthPing(b)
		if !ok {
			return
		}
		buf := make([]byte, HealthPktSize)
		EncodeHealthPong(buf, p)
		_ = mux.SendOOB(buf)
	})
}
