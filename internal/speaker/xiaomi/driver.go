package xiaomi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/andy/vox-mi-agent/internal/speaker"
)

// Driver implements speaker.Driver on the Xiaomi cloud.
type Driver struct {
	acct *Account
	mina *Mina

	mu      sync.Mutex
	devs    map[string]MinaDevice // by deviceID, from the last device_list
	lastNLP map[string]string     // M01: last seen query per device
}

var _ speaker.Driver = (*Driver)(nil)

// NewDriver creates the driver (and its account).
func NewDriver(o Options) *Driver {
	a := NewAccount(o)
	return &Driver{acct: a, mina: NewMina(a), devs: map[string]MinaDevice{}, lastNLP: map[string]string{}}
}

// Account returns the account (login / OTP / status).
func (d *Driver) Account() *Account { return d.acct }

// Name implements speaker.Driver.
func (d *Driver) Name() string { return "xiaomi" }

func (d *Driver) refresh(ctx context.Context) ([]MinaDevice, error) {
	l, err := d.mina.DeviceList(ctx)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.devs = make(map[string]MinaDevice, len(l))
	for _, x := range l {
		d.devs[x.DeviceID] = x
	}
	d.mu.Unlock()
	return l, nil
}

func (d *Driver) info(ctx context.Context, id string) MinaDevice {
	d.mu.Lock()
	x, ok := d.devs[id]
	d.mu.Unlock()
	if !ok {
		if _, err := d.refresh(ctx); err == nil {
			d.mu.Lock()
			x = d.devs[id]
			d.mu.Unlock()
		}
	}
	return x
}

func (d *Driver) hardware(ctx context.Context, dev speaker.Device) string {
	if dev.Hardware != "" {
		return dev.Hardware
	}
	return d.info(ctx, dev.DeviceID).Hardware
}

// DeviceList implements speaker.Driver.
func (d *Driver) DeviceList(ctx context.Context) ([]speaker.Device, error) {
	l, err := d.refresh(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]speaker.Device, 0, len(l))
	for _, x := range l {
		online := x.Presence == "" || strings.EqualFold(x.Presence, "online")
		out = append(out, speaker.Device{DeviceID: x.DeviceID, Name: firstNonEmpty(x.Alias, x.Name), Hardware: x.Hardware,
			PlayMode: "auto", TTSMode: "auto", Online: &online})
	}
	return out, nil
}

// GetLatestAsk implements speaker.Driver.
func (d *Driver) GetLatestAsk(ctx context.Context, dev speaker.Device, since time.Time) (*speaker.Ask, error) {
	hw := d.hardware(ctx, dev)
	if askByUbus[strings.ToUpper(hw)] {
		q, err := d.mina.NLPResult(ctx, dev.DeviceID)
		if err != nil {
			return nil, err
		}
		d.mu.Lock()
		prev, seen := d.lastNLP[dev.DeviceID]
		d.lastNLP[dev.DeviceID] = q
		d.mu.Unlock()
		if !seen || q == "" || q == prev {
			return nil, nil // first observation is the baseline
		}
		return &speaker.Ask{DeviceID: dev.DeviceID, Query: q, Time: time.Now()}, nil
	}
	recs, err := d.mina.Conversation(ctx, dev.DeviceID, hw)
	if err != nil {
		return nil, err
	}
	var best *Record
	for i := range recs {
		if recs[i].Query != "" && recs[i].Time.After(since) && (best == nil || recs[i].Time.After(best.Time)) {
			best = &recs[i]
		}
	}
	if best == nil {
		return nil, nil
	}
	return &speaker.Ask{DeviceID: dev.DeviceID, Query: best.Query, Time: best.Time, Answer: best.Answer}, nil
}

// TextToSpeech implements speaker.Driver.
func (d *Driver) TextToSpeech(ctx context.Context, dev speaker.Device, text string) error {
	hw := d.hardware(ctx, dev)
	iid := MiotTTSIID(dev.MiotTTS, hw)
	if TTSMode(dev.TTSMode) == "miot" {
		return d.miotTTS(ctx, dev, iid, text)
	}
	err := d.mina.TTS(ctx, dev.DeviceID, text)
	if err != nil && (dev.TTSMode == "" || dev.TTSMode == "auto") && iid != "" {
		if err2 := d.miotTTS(ctx, dev, iid, text); err2 == nil {
			return nil
		}
	}
	return err
}

func (d *Driver) miotTTS(ctx context.Context, dev speaker.Device, iid, text string) error {
	if iid == "" {
		return fmt.Errorf("miot tts: no siid-aiid known for %q, set miot_tts", dev.Hardware)
	}
	siid, aiid, err := ParseIID(iid)
	if err != nil {
		return err
	}
	did := d.info(ctx, dev.DeviceID).MiotDID
	if did == "" {
		return fmt.Errorf("miot tts: device %s has no miot did", dev.DeviceID)
	}
	return d.mina.MiotAction(ctx, did, siid, aiid, []any{text})
}

// PlayByURL implements speaker.Driver.
func (d *Driver) PlayByURL(ctx context.Context, dev speaker.Device, url string) error {
	if PlayMode(dev.PlayMode, d.hardware(ctx, dev)) == "music" {
		return d.mina.PlayMusic(ctx, dev.DeviceID, url)
	}
	return d.mina.PlayURL(ctx, dev.DeviceID, url)
}

// PlayerSetVolume implements speaker.Driver.
func (d *Driver) PlayerSetVolume(ctx context.Context, dev speaker.Device, v int) error {
	return d.mina.SetVolume(ctx, dev.DeviceID, v)
}

// PlayerPause implements speaker.Driver.
func (d *Driver) PlayerPause(ctx context.Context, dev speaker.Device) error {
	return d.mina.Operation(ctx, dev.DeviceID, "pause")
}

// PlayerPlay implements speaker.Driver.
func (d *Driver) PlayerPlay(ctx context.Context, dev speaker.Device) error {
	return d.mina.Operation(ctx, dev.DeviceID, "play")
}

// PlayerStop implements speaker.Driver.
func (d *Driver) PlayerStop(ctx context.Context, dev speaker.Device) error {
	return d.mina.Operation(ctx, dev.DeviceID, "stop")
}

// PlayerGetStatus implements speaker.Driver.
func (d *Driver) PlayerGetStatus(ctx context.Context, dev speaker.Device) (speaker.Status, error) {
	s, v, err := d.mina.PlayStatus(ctx, dev.DeviceID)
	return speaker.Status{Status: s, Volume: v}, err
}
