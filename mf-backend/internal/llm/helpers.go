package llm

import (
	"encoding/json"
	"time"
)

// This file holds small serialization helpers used by the store. Keeping them
// separate keeps the SQL in store.go readable.

// marshalMeta converts a metadata map to JSON bytes for a jsonb column,
// defaulting to an empty object so the column is never NULL.
func marshalMeta(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

func unmarshalMeta(b []byte) map[string]any {
	out := map[string]any{}
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func unmarshalBreakdown(b []byte) map[string]float64 {
	out := map[string]float64{}
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

// pgxTime wraps *time.Time so a LEFT JOIN's NULL score timestamp scans cleanly.
type pgxTime struct{ t *time.Time }

func (p *pgxTime) Scan(src any) error {
	if src == nil {
		p.t = nil
		return nil
	}
	if tv, ok := src.(time.Time); ok {
		p.t = &tv
	}
	return nil
}

func (p pgxTime) Time() time.Time {
	if p.t == nil {
		return time.Time{}
	}
	return *p.t
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
