// Package bridge controls configured speakers through their local HTTP bridge.
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/speaker"
)

const (
	controlTimeout = 15 * time.Second
	maxResponse    = 1 << 20
)

// Endpoint associates a stable local device ID with an authenticated bridge.
type Endpoint struct {
	Device                  speaker.Device
	URL, Username, Password string
}

// Driver uses no Xiaomi account or cloud device discovery.
type Driver struct {
	endpoints []Endpoint
	client    *http.Client
}

var _ speaker.Driver = (*Driver)(nil)

// NewDriver copies the configured endpoints. Invalid endpoints fail on use.
func NewDriver(endpoints []Endpoint) *Driver {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = controlTimeout
	return &Driver{
		endpoints: append([]Endpoint(nil), endpoints...),
		client:    &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (*Driver) Name() string { return "bridge" }

func (d *Driver) DeviceList(ctx context.Context) ([]speaker.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	devices := make([]speaker.Device, 0, len(d.endpoints))
	for _, ep := range d.endpoints {
		devices = append(devices, ep.Device)
	}
	return devices, nil
}

func (d *Driver) endpoint(dev speaker.Device) (Endpoint, error) {
	var found *Endpoint
	for i := range d.endpoints {
		if d.endpoints[i].Device.DeviceID == dev.DeviceID {
			if found != nil {
				return Endpoint{}, errors.New("duplicate bridge device ID")
			}
			found = &d.endpoints[i]
		}
	}
	if found == nil {
		return Endpoint{}, speaker.ErrNoDevice
	}
	ep := *found
	u, err := url.Parse(ep.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || ep.Device.DeviceID == "" || strings.TrimSpace(ep.Username) == "" || ep.Password == "" || strings.Contains(ep.Username, ":") {
		return Endpoint{}, errors.New("invalid bridge endpoint or missing explicit credentials")
	}
	return ep, nil
}

func (d *Driver) request(ctx context.Context, dev speaker.Device, method, path string, body any) (*http.Response, error) {
	ep, err := d.endpoint(dev)
	if err != nil {
		return nil, err
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(ep.URL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("invalid bridge request")
	}
	req.SetBasicAuth(ep.Username, ep.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if path == "/v1/asr/stream" {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := d.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Do not return url.Error: its URL may contain sensitive playback input.
		return nil, errors.New("bridge HTTP request failed")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("bridge HTTP status %d", resp.StatusCode)
	}
	return resp, nil
}

func (d *Driver) control(ctx context.Context, dev speaker.Device, method, path string, body any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	resp, err := d.request(ctx, dev, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return errors.New("bridge response is not JSON")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponse {
		return errors.New("bridge response too large")
	}
	var envelope struct {
		OK      *bool  `json:"ok"`
		Error   string `json:"error"`
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("invalid bridge JSON: %w", err)
	}
	if envelope.OK == nil || !*envelope.OK || envelope.Error != "" || envelope.Warning != "" {
		// Remote error text is untrusted and may contain credentials or URLs.
		return errors.New("bridge reported command failure")
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// GetLatestAsk is intentionally unsupported: ASR uses the live, non-replaying SSE stream.
func (*Driver) GetLatestAsk(context.Context, speaker.Device, time.Time) (*speaker.Ask, error) {
	return nil, speaker.ErrNotImplemented
}

func (d *Driver) TextToSpeech(ctx context.Context, dev speaker.Device, text string) error {
	return d.control(ctx, dev, http.MethodPost, "/v1/tts", struct {
		Text string `json:"text"`
		Wait bool   `json:"wait"`
	}{text, false}, nil)
}
func (d *Driver) PlayByURL(ctx context.Context, dev speaker.Device, url string) error {
	return d.control(ctx, dev, http.MethodPost, "/v1/play-url", map[string]string{"url": url}, nil)
}
func (d *Driver) PlayerSetVolume(ctx context.Context, dev speaker.Device, volume int) error {
	if volume < 0 || volume > 100 {
		return errors.New("volume must be between 0 and 100")
	}
	return d.control(ctx, dev, http.MethodPost, "/v1/set-volume", map[string]int{"volume": volume}, nil)
}
func (d *Driver) PlayerPause(ctx context.Context, dev speaker.Device) error {
	return d.control(ctx, dev, http.MethodPost, "/v1/pause", nil, nil)
}
func (d *Driver) PlayerPlay(ctx context.Context, dev speaker.Device) error {
	return d.control(ctx, dev, http.MethodPost, "/v1/play", nil, nil)
}
func (d *Driver) PlayerStop(ctx context.Context, dev speaker.Device) error {
	return d.control(ctx, dev, http.MethodPost, "/v1/stop", nil, nil)
}
func (d *Driver) PlayerGetStatus(ctx context.Context, dev speaker.Device) (speaker.Status, error) {
	var out struct {
		Status *struct {
			TTS *struct {
				Speaking *bool `json:"speaking"`
			} `json:"tts"`
			Player      string `json:"player"`
			PlayerError string `json:"player_error"`
		} `json:"status"`
	}
	if err := d.control(ctx, dev, http.MethodGet, "/v1/status", nil, &out); err != nil {
		return speaker.Status{}, err
	}
	if out.Status == nil || out.Status.TTS == nil || out.Status.TTS.Speaking == nil {
		return speaker.Status{}, errors.New("bridge status missing playback state")
	}
	if *out.Status.TTS.Speaking {
		return speaker.Status{Status: speaker.StatusPlaying}, nil
	}
	if out.Status.PlayerError != "" {
		return speaker.Status{}, errors.New("bridge player status failed")
	}
	switch out.Status.Player {
	case "playing":
		return speaker.Status{Status: speaker.StatusPlaying}, nil
	case "paused":
		return speaker.Status{Status: speaker.StatusPaused}, nil
	case "idle":
		return speaker.Status{Status: speaker.StatusIdle}, nil
	default:
		return speaker.Status{}, errors.New("unknown bridge player status")
	}
}
