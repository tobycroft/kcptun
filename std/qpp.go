// No trouble

package std

import (
	"io"

	"github.com/xtaci/qpp"
)

// QPPPort implements io.ReadWriteCloser interface for Quantum Permutation Pads
type QPPPort struct {
	underlying io.ReadWriteCloser // io.Writer is not enough, we need to close the underlying writer as well

	pad   *qpp.QuantumPermutationPad
	wprng *qpp.Rand
	rprng *qpp.Rand
}

func NewQPPPort(underlying io.ReadWriteCloser, pad *qpp.QuantumPermutationPad, seed []byte) *QPPPort {
	wprng := qpp.CreatePRNG(seed)
	rprng := qpp.CreatePRNG(seed)
	return &QPPPort{underlying, pad, wprng, rprng}
}

func (r *QPPPort) Read(p []byte) (n int, err error) {
	n, err = r.underlying.Read(p)
	r.pad.DecryptWithPRNG(p[:n], r.rprng)
	return
}

func (r *QPPPort) Write(p []byte) (n int, err error) {
	r.pad.EncryptWithPRNG(p, r.wprng)
	return r.underlying.Write(p)
}

func (r *QPPPort) Close() error {
	return r.underlying.Close()
}
