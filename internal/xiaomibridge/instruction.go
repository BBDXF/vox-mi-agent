package xiaomibridge

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Instruction is one line of the instruction log: an AIVS directive
// ({"header":{...},"payload":{...}}).
type Instruction struct {
	Header struct {
		DialogID  string `json:"dialog_id"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"header"`
	Payload json.RawMessage `json:"payload"`
}

// Recognition is the text of a SpeechRecognizer.RecognizeResult directive.
type Recognition struct {
	DialogID string
	Text     string
	Final    bool
}

// ParseInstruction decodes a log line. Lines with a non-JSON prefix are
// accepted from the first '{'.
func ParseInstruction(line []byte) (Instruction, bool) {
	var ins Instruction
	i := bytes.IndexByte(line, '{')
	if i < 0 || json.Unmarshal(line[i:], &ins) != nil || ins.Header.Name == "" {
		return Instruction{}, false
	}
	return ins, true
}

// ParseRecognition extracts recognized speech from a log line. ok is false for
// any other directive and for results without text.
func ParseRecognition(line []byte) (Recognition, bool) {
	ins, ok := ParseInstruction(line)
	if !ok || ins.Header.Namespace != "SpeechRecognizer" || ins.Header.Name != "RecognizeResult" {
		return Recognition{}, false
	}
	var p struct {
		IsFinal bool `json:"is_final"`
		Results []struct {
			Text string `json:"text"`
		} `json:"results"`
	}
	if json.Unmarshal(ins.Payload, &p) != nil || len(p.Results) == 0 {
		return Recognition{}, false
	}
	text := strings.TrimSpace(p.Results[0].Text)
	if text == "" {
		return Recognition{}, false
	}
	return Recognition{DialogID: ins.Header.DialogID, Text: text, Final: p.IsFinal}, true
}
