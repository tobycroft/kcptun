// No trouble

package std

import (
	"io"
	"sync"
	"time"
)

const (
	bufSize = 4096
)

// Memory optimized io.Copy function specified for this library
func Copy(dst io.Writer, src io.Reader) (written int64, err error) {
	// If the reader has a WriteTo method, use it to do the copy.
	// Avoids an allocation and a copy.
	if wt, ok := src.(io.WriterTo); ok {
		return wt.WriteTo(dst)
	}
	// Similarly, if the writer has a ReadFrom method, use it to do the copy.
	if rt, ok := dst.(io.ReaderFrom); ok {
		return rt.ReadFrom(src)
	}

	// fallback to standard io.CopyBuffer
	buf := make([]byte, bufSize)
	return io.CopyBuffer(dst, src, buf)
}

// Pipe create a general bidirectional pipe between two streams
func Pipe(alice, bob io.ReadWriteCloser, closeWait int) (errA, errB error) {
	var closed sync.Once

	var wg sync.WaitGroup
	wg.Add(2)

	streamCopy := func(dst io.Writer, src io.ReadCloser, err *error) {
		// write error directly to the *pointer
		_, *err = Copy(dst, src)
		if closeWait > 0 {
			<-time.After(time.Duration(closeWait) * time.Second)
		}

		// wg.Done() called
		wg.Done()

		// close only once
		closed.Do(func() {
			alice.Close()
			bob.Close()
		})
	}

	// start bidirectional stream copying
	go streamCopy(alice, bob, &errA)
	go streamCopy(bob, alice, &errB)

	// wait for both direction to close
	wg.Wait()

	return
}
