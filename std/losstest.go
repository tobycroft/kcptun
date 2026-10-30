// No trouble

package std

import (
	"bytes"
	"encoding/binary"
	"sync"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

// Loss test protocol carried in KCP OOB packets. OOB packets are unreliable
// by design (no retransmission, no ACK), so they measure the raw UDP path
// instead of the reliable KCP stream.
//
// Uplink and downlink are measured in two sequential phases:
//
//	phase 1 downlink: the client asks the server to send probes, the client
//	                  counts what arrives, so the loss covers only the
//	                  server -> client path;
//	phase 2 uplink:   the client sends probes, the server counts what it
//	                  receives and reports the number back on request, so the
//	                  loss covers only the client -> server path. Echoed
//	                  probes additionally provide RTT / jitter.
//
// Probe packet (40B header + padding):
//
//	| magic (8B) | test id (8B) | seq (8B) | send timestamp (8B) | dir (1B) | pad |
//
// Control packet (48B):
//
//	| magic (8B) | test id (8B) | type (1B) | phase (1B) | pad (6B) | x (8B) | y (8B) | z (8B) |
const (
	// LossTestMagic marks an OOB payload as a loss test probe.
	LossTestMagic = "KCPLTST1"
	// LossTestCtrlMagic marks an OOB payload as a loss test control packet.
	LossTestCtrlMagic = "KCPLTSC1"
	// LossTestMagicSize is len(LossTestMagic).
	LossTestMagicSize = 8
	// LossTestHeaderSize is probe magic + test id + seq + timestamp + dir.
	LossTestHeaderSize = 40
	// LossTestCtrlSize is the fixed size of a control packet.
	LossTestCtrlSize = 48
	// LossTestMaxPayload bounds the payload of probes and echoed probes.
	LossTestMaxPayload = 1024
)

// Probe direction.
const (
	// LossTestDirUplink marks probes sent by the client.
	LossTestDirUplink byte = 1
	// LossTestDirDownlink marks probes sent by the server.
	LossTestDirDownlink byte = 2
)

// Control packet types.
const (
	// LossTestCtrlDownlinkStart asks the server to start the downlink sender;
	// x = duration in seconds, y = packets per second, z = payload size.
	LossTestCtrlDownlinkStart byte = 1
	// LossTestCtrlUplinkEnd asks the server to report its uplink receive count.
	LossTestCtrlUplinkEnd byte = 2
	// LossTestCtrlReport carries the server-side counters;
	// x = probes received by the server, y = probes sent by the server.
	LossTestCtrlReport byte = 3
)

var (
	lossTestMagic     = []byte(LossTestMagic)
	lossTestCtrlMagic = []byte(LossTestCtrlMagic)
)

// LossTestPacket is a decoded loss test probe.
type LossTestPacket struct {
	ID   uint64
	Seq  uint64
	Sent time.Time
	Dir  byte
}

// EncodeLossTestPacket fills buf with a probe packet and returns the number of
// bytes used. buf must be at least LossTestHeaderSize bytes.
func EncodeLossTestPacket(buf []byte, id, seq uint64, sent time.Time, dir byte) int {
	if len(buf) < LossTestHeaderSize {
		return 0
	}
	copy(buf[:LossTestMagicSize], lossTestMagic)
	binary.LittleEndian.PutUint64(buf[8:], id)
	binary.LittleEndian.PutUint64(buf[16:], seq)
	binary.LittleEndian.PutUint64(buf[24:], uint64(sent.UnixNano()))
	buf[32] = dir
	return LossTestHeaderSize
}

// DecodeLossTestPacket parses a probe payload, ok is false for non-probe
// traffic. Probes written by older clients without the direction byte are
// reported as uplink probes.
func DecodeLossTestPacket(b []byte) (p LossTestPacket, ok bool) {
	if len(b) < LossTestMagicSize+24 || !bytes.Equal(b[:LossTestMagicSize], lossTestMagic) {
		return p, false
	}
	p.ID = binary.LittleEndian.Uint64(b[8:])
	p.Seq = binary.LittleEndian.Uint64(b[16:])
	p.Sent = time.Unix(0, int64(binary.LittleEndian.Uint64(b[24:])))
	if len(b) > 32 {
		p.Dir = b[32]
	}
	if p.Dir == 0 {
		p.Dir = LossTestDirUplink
	}
	return p, true
}

// LossTestCtrl is a decoded loss test control packet.
type LossTestCtrl struct {
	ID    uint64
	Type  byte
	Phase byte
	X     uint64
	Y     uint64
	Z     uint64
}

// EncodeLossTestCtrl fills buf with a control packet and returns the number of
// bytes used. buf must be at least LossTestCtrlSize bytes.
func EncodeLossTestCtrl(buf []byte, id uint64, typ, phase byte, x, y, z uint64) int {
	if len(buf) < LossTestCtrlSize {
		return 0
	}
	copy(buf[:LossTestMagicSize], lossTestCtrlMagic)
	binary.LittleEndian.PutUint64(buf[8:], id)
	buf[16] = typ
	buf[17] = phase
	binary.LittleEndian.PutUint64(buf[24:], x)
	binary.LittleEndian.PutUint64(buf[32:], y)
	binary.LittleEndian.PutUint64(buf[40:], z)
	return LossTestCtrlSize
}

// DecodeLossTestCtrl parses a control payload, ok is false for non-control
// traffic.
func DecodeLossTestCtrl(b []byte) (c LossTestCtrl, ok bool) {
	if len(b) < LossTestCtrlSize || !bytes.Equal(b[:LossTestMagicSize], lossTestCtrlMagic) {
		return c, false
	}
	c.ID = binary.LittleEndian.Uint64(b[8:])
	c.Type = b[16]
	c.Phase = b[17]
	c.X = binary.LittleEndian.Uint64(b[24:])
	c.Y = binary.LittleEndian.Uint64(b[32:])
	c.Z = binary.LittleEndian.Uint64(b[40:])
	return c, true
}

// RegisterLossTestServer installs the OOB handler that serves loss tests on a
// freshly accepted session: uplink probes are counted and echoed, the
// downlink sender is started on request, and control packets are answered
// with counter reports. OOB requires FEC, so an error is returned when the
// session has no FEC encoder.
func RegisterLossTestServer(conn *kcp.UDPSession) error {
	var mu sync.Mutex
	uplink := make(map[uint64]uint64) // test id -> received uplink probe count
	started := make(map[uint64]bool)  // test ids with a running downlink sender

	sendCtrl := func(id uint64, typ, phase byte, x, y, z uint64) {
		buf := make([]byte, LossTestCtrlSize)
		EncodeLossTestCtrl(buf, id, typ, phase, x, y, z)
		// SendOOB only enqueues the packet, it never blocks.
		_ = conn.SendOOB(buf)
	}

	// runDownlink sends downlink probes at the requested rate and reports the
	// total number of probes sent when done.
	runDownlink := func(id uint64, duration, pps, size int) {
		if duration <= 0 {
			duration = 30
		}
		if duration > 300 {
			duration = 300
		}
		if size < LossTestHeaderSize {
			size = LossTestHeaderSize
		}
		if size > LossTestMaxPayload {
			size = LossTestMaxPayload
		}
		if pps <= 0 {
			pps = 20000
		}
		if pps > 500000 {
			pps = 500000
		}
		interval := time.Second / time.Duration(pps)
		batch := 1
		if interval < time.Millisecond {
			batch = int(time.Millisecond / interval)
			interval = time.Millisecond
		}
		buf := make([]byte, size)
		deadline := time.Now().Add(time.Duration(duration) * time.Second)
		var seq, sent uint64
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
	drain:
		for time.Now().Before(deadline) {
			for i := 0; i < batch; i++ {
				seq++
				EncodeLossTestPacket(buf, id, seq, time.Now(), LossTestDirDownlink)
				if err := conn.SendOOB(buf); err != nil {
					break drain // session gone, stop the sender
				}
				sent++
			}
			<-ticker.C
		}
		sendCtrl(id, LossTestCtrlReport, LossTestDirDownlink, 0, sent, 0)
	}

	return conn.SetOOBHandler(func(b []byte) {
		if len(b) > LossTestMaxPayload {
			return
		}
		if c, ok := DecodeLossTestCtrl(b); ok {
			switch c.Type {
			case LossTestCtrlDownlinkStart:
				mu.Lock()
				first := !started[c.ID]
				started[c.ID] = true
				mu.Unlock()
				if first {
					go runDownlink(c.ID, int(c.X), int(c.Y), int(c.Z))
				}
			case LossTestCtrlUplinkEnd:
				mu.Lock()
				received := uplink[c.ID]
				mu.Unlock()
				// Idempotent: repeated requests re-report the same counters.
				sendCtrl(c.ID, LossTestCtrlReport, LossTestDirUplink, received, received, 0)
			}
			return
		}
		if p, ok := DecodeLossTestPacket(b); ok && p.Dir == LossTestDirUplink {
			mu.Lock()
			uplink[p.ID]++
			mu.Unlock()
			// Echo the probe back so the client can measure RTT.
			_ = conn.SendOOB(b)
		}
	})
}
