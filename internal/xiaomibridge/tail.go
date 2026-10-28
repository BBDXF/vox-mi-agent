// Package xiaomibridge is the on-speaker side of a flashed Xiaomi speaker: it
// follows the XiaoAi instruction log, pushes recognized user speech to a URL,
// and exposes a small authenticated HTTP API for local TTS / playback / stop.
package xiaomibridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"time"
)

// DefaultInstructionLog is where mico_aivs_lab writes the directives the cloud
// sends to the speaker (one JSON object per line).
const DefaultInstructionLog = "/tmp/mico_aivs_lab/instruction.log"

// maxLine bounds a single unterminated line; longer garbage is dropped.
const maxLine = 1 << 20

// Tailer follows a growing text file by polling. It survives the file not
// existing yet, being truncated, and being replaced (new inode).
type Tailer struct {
	Path  string
	Every time.Duration
	// FromStart emits the content present when the file is first opened;
	// otherwise the tail starts at the current end (history is skipped).
	// Files that appear or are replaced later are always read from offset 0.
	FromStart bool
}

// Run calls emit for every complete line (without the trailing newline) until
// ctx is done. The slice passed to emit is only valid during the call.
func (t *Tailer) Run(ctx context.Context, emit func(line []byte)) error {
	every := t.Every
	if every <= 0 {
		every = 10 * time.Millisecond
	}
	var (
		f       *os.File
		fi      os.FileInfo
		pos     int64
		pending []byte
		first   = true
		buf     = make([]byte, 64<<10)
	)
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	flush := func(data []byte) {
		pending = append(pending, data...)
		for {
			i := bytes.IndexByte(pending, '\n')
			if i < 0 {
				break
			}
			if line := bytes.TrimRight(pending[:i], "\r"); len(line) > 0 {
				emit(line)
			}
			pending = pending[i+1:]
		}
		if len(pending) > maxLine {
			pending = pending[:0]
		}
		if len(pending) == 0 {
			pending = nil // let a large drained buffer be collected
		}
	}
	drain := func() error {
		for {
			n, err := f.ReadAt(buf, pos)
			if n > 0 {
				pos += int64(n)
				flush(buf[:n])
			}
			if errors.Is(err, io.EOF) || (err == nil && n == 0) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		cur, err := os.Stat(t.Path)
		switch {
		case err == nil && f == nil:
			if nf, oerr := os.Open(t.Path); oerr == nil {
				f, fi, pos, pending = nf, cur, 0, nil
				if first && !t.FromStart {
					pos = cur.Size()
				}
				first = false
			}
		case err == nil && !os.SameFile(fi, cur):
			_ = drain() // finish the old file before switching
			f.Close()
			f, fi, pos, pending = nil, nil, 0, nil
			continue
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return err
		}
		if f != nil {
			if st, serr := f.Stat(); serr == nil && st.Size() < pos {
				pos, pending = 0, nil // truncated in place
			}
			if err := drain(); err != nil {
				return err
			}
		}
		first = false
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
