// No trouble

package main

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/tobycroft/kcptun/std"
	kcp "github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

const (
	lossTestLowPPS = 100
	lossTestMidPPS = 1000
	lossTestDrain  = 2 * time.Second
	lossTestHistUS = 20000 // 100us buckets up to 2s
)

// lossTestStats collects echo receptions from the OOB handler.
type lossTestStats struct {
	mu       sync.Mutex
	bitmap   []uint64 // received seq bitmap
	received uint64
	rttMin   time.Duration
	rttMax   time.Duration
	rttSum   time.Duration
	rttCount uint64
	hist     [lossTestHistUS + 1]uint64 // RTT histogram, 100us buckets
	jitter   float64
	lastRTT  time.Duration
	haveLast bool
}

func (s *lossTestStats) record(seq uint64, rtt time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := seq / 64
	if idx >= uint64(len(s.bitmap)) {
		size := len(s.bitmap)
		for uint64(size) <= idx {
			size *= 2
		}
		next := make([]uint64, size)
		copy(next, s.bitmap)
		s.bitmap = next
	}
	mask := uint64(1) << (seq % 64)
	if s.bitmap[idx]&mask == 0 {
		s.bitmap[idx] |= mask
		s.received++
	}

	if s.haveLast {
		d := rtt - s.lastRTT
		if d < 0 {
			d = -d
		}
		// RFC3550 style jitter estimation
		s.jitter += (float64(d) - s.jitter) / 16
	}
	s.lastRTT = rtt
	s.haveLast = true

	if s.rttCount == 0 || rtt < s.rttMin {
		s.rttMin = rtt
	}
	if rtt > s.rttMax {
		s.rttMax = rtt
	}
	s.rttSum += rtt
	s.rttCount++

	us := int(rtt / time.Microsecond)
	if us > lossTestHistUS {
		us = lossTestHistUS
	}
	s.hist[us]++
}

// percentileLocked must be called with s.mu held.
func (s *lossTestStats) percentileLocked(p float64) time.Duration {
	if s.rttCount == 0 {
		return 0
	}
	target := uint64(float64(s.rttCount) * p)
	if target == 0 {
		target = 1
	}
	var cum uint64
	for us, n := range s.hist {
		cum += n
		if cum >= target {
			return time.Duration(us) * time.Microsecond
		}
	}
	return lossTestHistUS * time.Microsecond
}

func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
}

// waitLossTestReport waits for a report control packet of the given phase,
// re-sending the trigger through resend every retry interval because OOB
// packets are unreliable. Returns nil on timeout.
func waitLossTestReport(ch chan std.LossTestCtrl, phase byte, timeout, retry time.Duration, resend func()) *std.LossTestCtrl {
	deadline := time.After(timeout)
	ticker := time.NewTicker(retry)
	defer ticker.Stop()
	for {
		select {
		case c := <-ch:
			if c.Phase == phase {
				cc := c
				return &cc
			}
		case <-ticker.C:
			resend()
		case <-deadline:
			return nil
		}
	}
}

// runLossTest measures packet loss in two sequential phases and exits.
// Phase 1 (downlink): the server sends OOB probes to the client, the client
// counts what arrives, so the result covers only the server -> client path.
// Phase 2 (uplink): the client sends OOB probes, the server counts what it
// receives and reports the number back, so the result covers only the
// client -> server path; echoed probes additionally provide RTT / jitter.
func runLossTest(config *Config, block kcp.BlockCrypt) error {
	if config.DataShard <= 0 || config.ParityShard <= 0 {
		return fmt.Errorf("losstest requires FEC: datashard and parityshard must both be > 0 on client and server")
	}

	duration := config.LossTestDuration
	if duration <= 0 {
		duration = 30
	}
	if duration > 300 {
		duration = 300
	}
	size := config.LossTestSize
	if size < std.LossTestHeaderSize {
		size = std.LossTestHeaderSize
	}
	if size > std.LossTestMaxPayload {
		size = std.LossTestMaxPayload
	}

	rate := strings.ToLower(config.LossTestRate)
	var pps int
	var interval time.Duration
	var batch int
	switch rate {
	case "low":
		pps = lossTestLowPPS
		interval = 10 * time.Millisecond
		batch = 1
	case "mid":
		pps = lossTestMidPPS
		interval = time.Millisecond
		batch = 1
	case "full":
		// OOB packets are silently dropped when the session's post-processing
		// queue is full, so cap the rate to keep local drops out of the result.
		pps = config.LossTestPPS
		if pps <= 0 {
			pps = 20000
		}
		if pps > 500000 {
			pps = 500000
		}
		interval = time.Millisecond
		batch = pps / 1000
		if batch < 1 {
			batch = 1
		}
	default:
		return fmt.Errorf("invalid losstest-rate %q, want low, mid or full", config.LossTestRate)
	}

	conn, err := dial(config, block)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	conn.SetStreamMode(true)
	conn.SetWriteDelay(false)
	conn.SetNoDelay(config.NoDelay, config.Interval, config.Resend, config.NoCongestion)
	conn.SetWindowSize(config.SndWnd, config.RcvWnd)
	conn.SetMtu(config.MTU)
	conn.SetACKNoDelay(config.AckNodelay)
	if err := conn.SetDSCP(config.DSCP); err != nil {
		log.Println("SetDSCP:", err)
	}
	if err := conn.SetReadBuffer(config.SockBuf); err != nil {
		log.Println("SetReadBuffer:", err)
	}
	if err := conn.SetWriteBuffer(config.SockBuf); err != nil {
		log.Println("SetWriteBuffer:", err)
	}

	if limit := conn.GetOOBMaxSize(); limit < size {
		return fmt.Errorf("losstest payload %d exceeds OOB limit %d (mtu %d)", size, limit, config.MTU)
	}

	// Keep a smux session alive so the server does not tear the KCP session
	// down while the test is running. No stream is opened.
	smuxConfig := smux.DefaultConfig()
	smuxConfig.Version = config.SmuxVer
	smuxConfig.MaxReceiveBuffer = config.SmuxBuf
	smuxConfig.MaxStreamBuffer = config.StreamBuf
	smuxConfig.KeepAliveInterval = time.Duration(config.KeepAlive) * time.Second
	if err := smux.VerifyConfig(smuxConfig); err != nil {
		return err
	}
	var session *smux.Session
	if config.NoComp {
		session, err = smux.Client(conn, smuxConfig)
	} else {
		session, err = smux.Client(std.NewCompStream(conn), smuxConfig)
	}
	if err != nil {
		return fmt.Errorf("create smux session: %w", err)
	}
	defer session.Close()

	testID := uint64(time.Now().UnixNano()) ^ rand.Uint64()
	dlStats := &lossTestStats{bitmap: make([]uint64, 4096)}
	ulStats := &lossTestStats{bitmap: make([]uint64, 4096)}
	reportCh := make(chan std.LossTestCtrl, 4)
	if err := conn.SetOOBHandler(func(b []byte) {
		if c, ok := std.DecodeLossTestCtrl(b); ok {
			if c.Type == std.LossTestCtrlReport && c.ID == testID {
				select {
				case reportCh <- c:
				default:
				}
			}
			return
		}
		if p, ok := std.DecodeLossTestPacket(b); ok && p.ID == testID {
			switch p.Dir {
			case std.LossTestDirDownlink:
				dlStats.record(p.Seq, time.Since(p.Sent))
			default:
				ulStats.record(p.Seq, time.Since(p.Sent))
			}
		}
	}); err != nil {
		return fmt.Errorf("enable OOB receive (FEC required): %w", err)
	}

	rateDesc := fmt.Sprintf("low (%d pps)", lossTestLowPPS)
	if rate == "mid" {
		rateDesc = fmt.Sprintf("mid (%d pps)", lossTestMidPPS)
	} else if rate == "full" {
		rateDesc = fmt.Sprintf("full (~%d pps ceiling)", pps)
	}

	fmt.Printf("==================== KCPTUN LOSS TEST ====================\n")
	fmt.Printf("target        : %v\n", conn.RemoteAddr())
	fmt.Printf("local         : %v\n", conn.LocalAddr())
	fmt.Printf("rate          : %s\n", rateDesc)
	fmt.Printf("payload       : %d bytes\n", size)
	fmt.Printf("duration      : %ds per direction\n", duration)

	// Give the server a moment to install the loss test handler after AcceptKCP.
	time.Sleep(500 * time.Millisecond)

	ctrl := make([]byte, std.LossTestCtrlSize)

	// ---- phase 1: downlink (server -> client) ----
	fmt.Printf("----------------------------------------------------------\n")
	fmt.Printf("phase 1       : downlink (server -> client)\n")
	sendStart := func() {
		std.EncodeLossTestCtrl(ctrl, testID, std.LossTestCtrlDownlinkStart, 0,
			uint64(duration), uint64(pps), uint64(size))
		_ = conn.SendOOB(ctrl)
	}
	sendStart()
	dlReport := waitLossTestReport(reportCh, std.LossTestDirDownlink,
		time.Duration(duration)*time.Second+lossTestDrain+10*time.Second,
		3*time.Second, sendStart)

	// Drain: keep receiving late probes before computing the result.
	time.Sleep(lossTestDrain)

	dlStats.mu.Lock()
	dlReceived := dlStats.received
	dlStats.mu.Unlock()
	if dlReport != nil {
		serverSent := dlReport.Y
		if dlReceived > serverSent {
			dlReceived = serverSent
		}
		lost := serverSent - dlReceived
		lossPct := 0.0
		if serverSent > 0 {
			lossPct = float64(lost) * 100 / float64(serverSent)
		}
		fmt.Printf("sent          : %d packets (by server)\n", serverSent)
		fmt.Printf("received      : %d packets (at client)\n", dlReceived)
		fmt.Printf("lost          : %d packets (%.3f%%)\n", lost, lossPct)
	} else {
		fmt.Printf("downlink      : no report from server, result unavailable\n")
		fmt.Printf("                (server may be running an older version)\n")
	}

	// ---- phase 2: uplink (client -> server) ----
	fmt.Printf("----------------------------------------------------------\n")
	fmt.Printf("phase 2       : uplink (client -> server)\n")
	buf := make([]byte, size)
	deadline := time.Now().Add(time.Duration(duration) * time.Second)
	started := time.Now()
	var sent, sendErrs, seq uint64
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		for i := 0; i < batch; i++ {
			seq++
			std.EncodeLossTestPacket(buf, testID, seq, time.Now(), std.LossTestDirUplink)
			if err := conn.SendOOB(buf); err != nil {
				sendErrs++
			}
			sent++
		}
		<-ticker.C
	}
	elapsed := time.Since(started)

	// Drain: keep receiving late echoes before computing the result.
	time.Sleep(lossTestDrain)

	sendEnd := func() {
		std.EncodeLossTestCtrl(ctrl, testID, std.LossTestCtrlUplinkEnd, 0, 0, 0, 0)
		_ = conn.SendOOB(ctrl)
	}
	sendEnd()
	ulReport := waitLossTestReport(reportCh, std.LossTestDirUplink,
		10*time.Second, time.Second, sendEnd)

	ulStats.mu.Lock()
	rttMin := ulStats.rttMin
	rttMax := ulStats.rttMax
	var rttAvg time.Duration
	if ulStats.rttCount > 0 {
		rttAvg = ulStats.rttSum / time.Duration(ulStats.rttCount)
	}
	p50 := ulStats.percentileLocked(0.50)
	p95 := ulStats.percentileLocked(0.95)
	p99 := ulStats.percentileLocked(0.99)
	rttCount := ulStats.rttCount
	jitter := time.Duration(ulStats.jitter)
	ulStats.mu.Unlock()

	if ulReport != nil {
		serverReceived := ulReport.X
		if serverReceived > sent {
			serverReceived = sent
		}
		lost := sent - serverReceived
		lossPct := 0.0
		if sent > 0 {
			lossPct = float64(lost) * 100 / float64(sent)
		}
		fmt.Printf("sent          : %d packets (by client)\n", sent)
		fmt.Printf("received      : %d packets (at server)\n", serverReceived)
		fmt.Printf("lost          : %d packets (%.3f%%)\n", lost, lossPct)
	} else {
		fmt.Printf("sent          : %d packets (by client)\n", sent)
		fmt.Printf("uplink        : no report from server, loss unavailable\n")
		fmt.Printf("                (server may be running an older version)\n")
	}
	if rttCount > 0 {
		fmt.Printf("rtt (echo)    : min %s / avg %s / p50 %s / p95 %s / p99 %s / max %s\n",
			formatDuration(rttMin), formatDuration(rttAvg), formatDuration(p50),
			formatDuration(p95), formatDuration(p99), formatDuration(rttMax))
		fmt.Printf("jitter        : %s\n", formatDuration(jitter))
	} else {
		fmt.Printf("rtt (echo)    : n/a (no echo received)\n")
	}
	fmt.Printf("actual rate   : %.0f pps over %s\n", float64(sent)/elapsed.Seconds(), elapsed.Round(time.Millisecond))
	fmt.Printf("send errors   : %d\n", sendErrs)
	if rttCount == 0 && sent > 0 {
		fmt.Printf("hint          : no echo received, make sure the server runs the\n")
		fmt.Printf("                matching version with FEC enabled (datashard/parityshard > 0)\n")
	}
	fmt.Printf("note          : OOB probes bypass KCP retransmission, so the loss rates\n")
	fmt.Printf("                reflect the raw UDP path; downlink is measured at the\n")
	fmt.Printf("                client, uplink is reported by the server; full rate may\n")
	fmt.Printf("                include local queue drops under saturation\n")
	fmt.Printf("==========================================================\n")

	return nil
}
