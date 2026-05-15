// No trouble

//go:build linux || darwin || freebsd

package std

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

const (
	EXIT_WAIT = 5 // max seconds to wait before exit
)

func init() {
	go sigHandler()
}

func sigHandler() {
	var exitOnce sync.Once
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)
	signal.Ignore(syscall.SIGPIPE)

	for {
		sig := <-ch
		switch sig {
		case syscall.SIGUSR1:
			log.Printf("KCP SNMP:%+v", kcp.DefaultSnmp.Copy())
		case syscall.SIGTERM, syscall.SIGINT:
			postProcess()
			signal.Stop(ch)
			syscall.Kill(syscall.Getpid(), syscall.SIGTERM)

			// wait for max EXIT_WAIT seconds before exit
			exitOnce.Do(func() {
				go func() {
					<-time.After(EXIT_WAIT * time.Second)
					os.Exit(0)
				}()
			})
		}
	}
}
