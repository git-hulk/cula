package runtime

import (
	"encoding/json"
	"strings"
)

type RawValue struct {
	raw json.RawMessage
}

func (r *RawValue) UnmarshalJSON(data []byte) error {
	r.raw = append(r.raw[:0], data...)
	return nil
}

func (r RawValue) Raw() json.RawMessage {
	return r.raw
}

func (r RawValue) AsString() (string, bool) {
	if len(r.raw) == 0 || r.raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(r.raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func (r RawValue) AsText() string {
	if s, ok := r.AsString(); ok {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if len(r.raw) > 0 && r.raw[0] == '[' && json.Unmarshal(r.raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if strings.TrimSpace(b.Text) != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
