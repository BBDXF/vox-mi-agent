package trace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/agent"
)

const (
	payloadPrefix = "trace-"
	payloadSuffix = ".jsonl"
	dateLayout    = "20060102"
)

// FileName returns the payload file name for the day of t (local time).
func FileName(t time.Time) string {
	return payloadPrefix + t.Local().Format(dateLayout) + payloadSuffix
}

// PayloadLog writes agent.Payload entries as JSON lines to
// <dir>/trace-YYYYMMDD.jsonl from one goroutine, rotating daily and pruning
// old files by age and total size.
type PayloadLog struct {
	dir string
	log *slog.Logger
	now func() time.Time

	ch    chan agent.Payload
	flush chan chan struct{}
	quit  chan struct{}
	done  chan struct{}

	enabled atomic.Bool
	dropped atomic.Int64

	mu            sync.Mutex // guards the settings below
	retentionDays int
	maxBytes      int64
	secrets       []string

	// writer-goroutine state
	f    *os.File
	bw   *bufio.Writer
	name string
	size int64
}

// NewPayloadLog starts the writer goroutine (disabled until Configure).
func NewPayloadLog(dir string, log *slog.Logger) *PayloadLog {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	p := &PayloadLog{
		dir: dir, log: log, now: time.Now,
		ch: make(chan agent.Payload, 256), flush: make(chan chan struct{}),
		quit: make(chan struct{}), done: make(chan struct{}),
		retentionDays: 7, maxBytes: 200 << 20,
	}
	go p.loop()
	return p
}

// Configure sets the live settings.
func (p *PayloadLog) Configure(enabled bool, retentionDays int, maxBytes int64) {
	p.mu.Lock()
	p.retentionDays, p.maxBytes = retentionDays, maxBytes
	p.mu.Unlock()
	p.enabled.Store(enabled)
}

// SetSecrets sets strings that are replaced by "***" in every written line
// (e.g. the LLM API key).
func (p *PayloadLog) SetSecrets(s ...string) {
	var keep []string
	for _, x := range s {
		if len(x) >= 6 {
			keep = append(keep, x)
		}
	}
	p.mu.Lock()
	p.secrets = keep
	p.mu.Unlock()
}

// Enabled reports whether payloads are being recorded.
func (p *PayloadLog) Enabled() bool { return p.enabled.Load() }

// Dropped is the number of entries dropped (buffer full / size cap).
func (p *PayloadLog) Dropped() int64 { return p.dropped.Load() }

// Write queues an entry (non-blocking; dropped when disabled or full).
func (p *PayloadLog) Write(e agent.Payload) {
	if !p.enabled.Load() {
		return
	}
	select {
	case p.ch <- e:
	default:
		p.dropped.Add(1)
	}
}

// Flush blocks until queued entries are on disk.
func (p *PayloadLog) Flush() {
	reply := make(chan struct{})
	select {
	case p.flush <- reply:
		<-reply
	case <-p.done:
	}
}

// Close flushes and stops the writer.
func (p *PayloadLog) Close() {
	select {
	case <-p.quit:
	default:
		close(p.quit)
	}
	<-p.done
}

func (p *PayloadLog) settings() (days int, max int64, secrets []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.retentionDays, p.maxBytes, p.secrets
}

func (p *PayloadLog) loop() {
	defer close(p.done)
	defer p.closeFile()
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	for {
		select {
		case e := <-p.ch:
			p.write(e)
		case <-tk.C:
			p.flushFile()
		case reply := <-p.flush:
			for drained := false; !drained; {
				select {
				case e := <-p.ch:
					p.write(e)
				default:
					drained = true
				}
			}
			p.flushFile()
			close(reply)
		case <-p.quit:
			for drained := false; !drained; {
				select {
				case e := <-p.ch:
					p.write(e)
				default:
					drained = true
				}
			}
			return
		}
	}
}

func (p *PayloadLog) write(e agent.Payload) {
	if e.TS.IsZero() {
		e.TS = p.now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		p.log.Warn("payload marshal failed", "err", err)
		return
	}
	_, maxBytes, secrets := p.settings()
	for _, s := range secrets {
		line = bytes.ReplaceAll(line, []byte(s), []byte("***"))
	}
	line = append(line, '\n')
	// rotation is decided by the wall clock, not the entry time, so that a
	// slow turn never reopens yesterday's (possibly pruned) file
	name := FileName(p.now())
	if name != p.name {
		p.closeFile()
		if err := os.MkdirAll(p.dir, 0o700); err != nil {
			p.log.Warn("payload dir", "err", err)
			return
		}
		f, err := os.OpenFile(filepath.Join(p.dir, name), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			p.log.Warn("payload open", "err", err)
			return
		}
		fi, _ := f.Stat()
		p.f, p.bw, p.name, p.size = f, bufio.NewWriterSize(f, 64<<10), name, 0
		if fi != nil {
			p.size = fi.Size()
		}
		p.Prune()
	}
	if maxBytes > 0 && p.size+int64(len(line)) > maxBytes {
		p.dropped.Add(1)
		return
	}
	n, err := p.bw.Write(line)
	p.size += int64(n)
	if err != nil {
		p.log.Warn("payload write", "err", err)
	}
}

func (p *PayloadLog) flushFile() {
	if p.bw != nil {
		_ = p.bw.Flush()
	}
}

func (p *PayloadLog) closeFile() {
	if p.f != nil {
		_ = p.bw.Flush()
		_ = p.f.Close()
		p.f, p.bw, p.name = nil, nil, ""
	}
}

type pfile struct {
	name string
	day  time.Time
	size int64
}

func (p *PayloadLog) files() []pfile {
	ents, err := os.ReadDir(p.dir)
	if err != nil {
		return nil
	}
	var out []pfile
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, payloadPrefix) || !strings.HasSuffix(n, payloadSuffix) {
			continue
		}
		day, err := time.ParseInLocation(dateLayout, strings.TrimSuffix(strings.TrimPrefix(n, payloadPrefix), payloadSuffix), time.Local)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, pfile{n, day, info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// Prune deletes payload files older than the retention days, then the oldest
// files while the total size exceeds the cap (the newest file is kept). It
// returns the number of files removed. Safe to call from any goroutine.
func (p *PayloadLog) Prune() int {
	days, maxBytes, _ := p.settings()
	files := p.files()
	removed := 0
	rm := func(f pfile) {
		if err := os.Remove(filepath.Join(p.dir, f.name)); err == nil {
			removed++
		}
	}
	if days > 0 {
		now := p.now().Local()
		cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -days)
		kept := files[:0]
		for _, f := range files {
			if f.day.Before(cutoff) {
				rm(f)
			} else {
				kept = append(kept, f)
			}
		}
		files = kept
	}
	if maxBytes > 0 {
		var total int64
		for _, f := range files {
			total += f.size
		}
		for len(files) > 1 && total > maxBytes {
			rm(files[0])
			total -= files[0].size
			files = files[1:]
		}
	}
	return removed
}

// ReadTurn returns the payload lines (raw JSON) of one turn. around is the
// turn's start time (zero = now); the files of that day and the next are
// scanned. Missing files yield an empty result.
func ReadTurn(dir, turnID string, around time.Time, limit int) ([]json.RawMessage, error) {
	if turnID == "" {
		return nil, errors.New("empty turn id")
	}
	if around.IsZero() {
		around = time.Now()
	}
	if limit <= 0 {
		limit = 500
	}
	needle, _ := json.Marshal(turnID)
	needle = append([]byte(`"turn_id":`), needle...)
	var out []json.RawMessage
	seen := map[string]bool{}
	for _, d := range []time.Time{around, around.AddDate(0, 0, 1)} {
		name := FileName(d)
		if seen[name] {
			continue
		}
		seen[name] = true
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return out, err
		}
		br := bufio.NewReaderSize(f, 1<<20)
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 && bytes.Contains(line, needle) && json.Valid(bytes.TrimSpace(line)) {
				out = append(out, json.RawMessage(bytes.TrimSpace(append([]byte(nil), line...))))
				if len(out) >= limit {
					f.Close()
					return out, nil
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					f.Close()
					return out, err
				}
				break
			}
		}
		f.Close()
	}
	return out, nil
}
