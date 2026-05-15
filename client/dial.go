// No trouble

package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/pkg/errors"
	"github.com/tobycroft/kcptun/std"
	kcp "github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/tcpraw"
)

// dial connects to the remote address
func dial(config *Config, block kcp.BlockCrypt) (*kcp.UDPSession, error) {
	mp, err := std.ParseMultiPort(config.RemoteAddr)
	if err != nil {
		return nil, err
	}

	// generate a random port
	var randport uint64
	err = binary.Read(rand.Reader, binary.LittleEndian, &randport)
	if err != nil {
		return nil, err
	}
	remoteAddr := fmt.Sprintf("%v:%v", mp.Host, uint64(mp.MinPort)+randport%uint64(mp.MaxPort-mp.MinPort+1))

	// emulate TCP connection
	if config.TCP {
		conn, err := tcpraw.Dial("tcp", remoteAddr)
		if err != nil {
			return nil, errors.Wrap(err, "tcpraw.Dial()")
		}

		udpaddr, err := net.ResolveUDPAddr("udp", remoteAddr)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		var convid uint32
		binary.Read(rand.Reader, binary.LittleEndian, &convid)
		return kcp.NewConn4(convid, udpaddr, block, config.DataShard, config.ParityShard, true, conn)
	}

	// default UDP connection
	return kcp.DialWithOptions(remoteAddr, block, config.DataShard, config.ParityShard)
}
