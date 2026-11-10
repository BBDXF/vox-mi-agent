package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/speaker"
)

const maxEvent = 64 << 10

var _ speaker.AskStreamer = (*Driver)(nil)

// StreamAsks opens one live subscription. Hook owns reconnect and backoff.
// Neither remote timestamps nor restartable sequence IDs are watermarks.
func (d *Driver) StreamAsks(ctx context.Context, dev speaker.Device, receive func(speaker.Ask)) error {
	if receive == nil {
		return errors.New("bridge ASR callback required")
	}
	resp, err := d.request(ctx, dev, http.MethodGet, "/v1/asr/stream", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "text/event-stream" {
		return errors.New("bridge response is not an SSE stream")
	}
	scan := bufio.NewScanner(resp.Body)
	scan.Buffer(make([]byte, 4096), maxEvent+1)
	var event string
	var data strings.Builder
	size := 0
	first := true
	var last time.Time
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scan.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		if line == "" {
			if (event == "asr" || event == "") && data.Len() != 0 {
				var ev struct {
					Type  string `json:"type"`
					Text  string `json:"text"`
					Final bool   `json:"final"`
				}
				if err := json.Unmarshal([]byte(data.String()), &ev); err != nil {
					return fmt.Errorf("invalid bridge ASR event: %w", err)
				}
				if ev.Type == "asr" && ev.Final && strings.TrimSpace(ev.Text) != "" {
					now := time.Now()
					if !now.After(last) {
						now = last.Add(time.Nanosecond)
					}
					last = now
					receive(speaker.Ask{DeviceID: dev.DeviceID, Query: ev.Text, Time: now})
				}
			}
			event = ""
			data.Reset()
			size = 0
			continue
		}
		size += len(line) + 1
		if size > maxEvent {
			return errors.New("bridge SSE event too large")
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := scan.Err(); err != nil {
		return fmt.Errorf("reading bridge SSE: %w", err)
	}
	// Incomplete events are never delivered; even a clean EOF needs reconnect.
	return io.EOF
}
