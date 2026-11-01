// No trouble

package std

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
)

// rollingRetainDiv controls how much of the limit is kept after a compaction:
// the file is trimmed down to limit/rollingRetainDiv instead of just enough room
// for the incoming line, so the expensive copy only happens once every
// limit/rollingRetainDiv bytes of logging instead of on every line.
const rollingRetainDiv = 2

// RollingLog is an io.Writer backed by a file that never grows beyond a fixed
// size. Once the limit is reached the oldest whole lines are dropped, so the
// file always holds the most recent tail of the log, still in chronological
// order, which keeps it readable with cat/vim/tail without any extra tooling.
//
// A limit of zero turns it into a plain append-only log file, which is what
// "no size configured" means.
type RollingLog struct {
	f     *os.File
	limit int64 // bytes, 0 means unlimited
	size  int64 // current file size, which is also the next write offset
	mu    sync.Mutex
}

// NewRollingLog opens path for writing and caps it at limitKB kilobytes. The
// file is truncated to size on open when it is already bigger than the limit.
func NewRollingLog(path string, limitKB int) (*RollingLog, error) {
	if path == "" {
		return nil, errors.New("std: rolling log needs a file path")
	}
	if limitKB < 0 {
		limitKB = 0
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}

	r := &RollingLog{f: f, limit: int64(limitKB) * 1024}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	r.size = info.Size()

	// an existing log left over from a previous run may well be over the limit
	if r.limit > 0 && r.size > r.limit {
		if err := r.compact(r.limit - 1); err != nil {
			f.Close()
			return nil, err
		}
	}
	return r, nil
}

// Write appends p, dropping the oldest lines first when the file is full. The
// log package writes one formatted line per call, so a write never splits a
// line in the middle of the log.
func (r *RollingLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.limit <= 0 {
		n, err := r.f.WriteAt(p, r.size)
		r.size += int64(n)
		return n, err
	}

	// a single record bigger than the whole file: keep only its newest tail,
	// starting at the first line boundary inside it
	if int64(len(p)) >= r.limit {
		tail := p[int64(len(p))-r.limit:]
		if i := bytes.IndexByte(tail, '\n'); i >= 0 && i+1 < len(tail) {
			tail = tail[i+1:]
		}
		if err := r.f.Truncate(0); err != nil {
			return 0, err
		}
		r.size = 0
		p = tail
	} else if r.size+int64(len(p)) > r.limit {
		if err := r.compact(r.limit / rollingRetainDiv); err != nil {
			return 0, err
		}
	}

	n, err := r.f.WriteAt(p, r.size)
	r.size += int64(n)
	return n, err
}

// Compact keeps the newest want bytes of the file, aligned to a line boundary.
// It writes the tail over the head and then shortens the file, so a crash in
// between leaves duplicated bytes at the end instead of losing the newest log.
func (r *RollingLog) compact(want int64) error {
	if want < 0 {
		want = 0
	}
	if want > r.limit {
		want = r.limit
	}
	if want > r.size {
		want = r.size
	}

	buf := make([]byte, want)
	n, err := r.f.ReadAt(buf, r.size-want)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	buf = buf[:n]

	// start at the first complete line, the byte before the cut may be in the
	// middle of a record
	if i := bytes.IndexByte(buf, '\n'); i >= 0 {
		buf = buf[i+1:]
	}

	if len(buf) > 0 {
		if _, err := r.f.WriteAt(buf, 0); err != nil {
			return err
		}
	}
	if err := r.f.Truncate(int64(len(buf))); err != nil {
		return err
	}
	r.size = int64(len(buf))
	return nil
}

// Size reports the current file size in bytes.
func (r *RollingLog) Size() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

// Close releases the underlying file.
func (r *RollingLog) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}
