// No trouble

package std

import (
	"bytes"
	"encoding/binary"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

// Loss test protocol carried in KCP OOB packets:
//
//	| magic (8B) | test id (8B) | seq (8B) | send timestamp (8B) | padding |
//
// OOB packets are unreliable by design (no retransmission, no ACK), so they
// measure the raw UDP path instead of the reliable KCP stream.
const (
	// LossTestMagic marks an OOB payload as a loss test probe.
	LossTestMagic = "KCPLTST1"
	// LossTestMagicSize is len(LossTestMagic).
	LossTestMagicSize = 8
	// LossTestHeaderSize is magic + test id + seq + timestamp.
	LossTestHeaderSize = 32
	// LossTestMaxPayload bounds the payload echoed by the server.
	LossTestMaxPayload = 1024
)

var lossTestMagic = []byte(LossTestMagic)

// EncodeLossTestPacket fills buf with a probe packet and returns the number of
// bytes used. buf must be at least LossTestHeaderSize bytes.
func EncodeLossTestPacket(buf []byte, id, seq uint64, sent time.Time) int {
	if len(buf) < LossTestHeaderSize {
		return 0
	}
	copy(buf[:LossTestMagicSize], lossTestMagic)
	binary.LittleEndian.PutUint64(buf[8:], id)
	binary.LittleEndian.PutUint64(buf[16:], seq)
	binary.LittleEndian.PutUint64(buf[24:], uint64(sent.UnixNano()))
	return len(buf)
}

// DecodeLossTestPacket parses an OOB payload, ok is false for non-probe traffic.
func DecodeLossTestPacket(b []byte) (id, seq uint64, sent time.Time, ok bool) {
	if len(b) < LossTestHeaderSize || !bytes.Equal(b[:LossTestMagicSize], lossTestMagic) {
		return 0, 0, time.Time{}, false
	}
	id = binary.LittleEndian.Uint64(b[8:])
	seq = binary.LittleEndian.Uint64(b[16:])
	sent = time.Unix(0, int64(binary.LittleEndian.Uint64(b[24:])))
	return id, seq, sent, true
}

// RegisterLossTestEcho installs an OOB handler that echoes loss test probes
// back to the sender. OOB requires FEC, so an error is returned when the
// session has no FEC encoder.
func RegisterLossTestEcho(conn *kcp.UDPSession) error {
	return conn.SetOOBHandler(func(b []byte) {
		if len(b) < LossTestHeaderSize || len(b) > LossTestMaxPayload {
			return
		}
		if !bytes.Equal(b[:LossTestMagicSize], lossTestMagic) {
			return
		}
		// SendOOB only enqueues the packet, it never blocks.
		_ = conn.SendOOB(b)
	})
}
