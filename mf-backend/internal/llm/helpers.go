package llm

import (
	"bytes"
	"encoding/json"
	"time"
)

// This file holds small serialization helpers used by the store. Keeping them
// separate keeps the SQL in store.go readable.

// emptyJSONObject is what a jsonb column gets when the client sent no metadata,
// so the column is never NULL and readers never have to special-case it.
var emptyJSONObject = []byte("{}")

// normalizeMeta prepares client metadata for a jsonb column. The bytes pass
// through untouched when present: the server never reads inside metadata, and
// Postgres validates the JSON on insert, so decoding and re-encoding it here
// would be pure allocation for no gain.
func normalizeMeta(m Metadata) []byte {
	if len(bytes.TrimSpace(m)) == 0 {
		return emptyJSONObject
	}
	return m
}

// metaOrEmpty keeps an absent jsonb value from reaching the client as a bare
// `null`, which would break callers that expect an object.
func metaOrEmpty(b []byte) Metadata {
	if len(b) == 0 {
		return emptyJSONObject
	}
	return b
}

// unmarshalBreakdown decodes the stored component scores. A malformed value
// yields a zeroed Breakdown rather than an error: a presentation detail of the
// score is not worth failing an otherwise good read over.
func unmarshalBreakdown(b []byte) Breakdown {
	var out Breakdown
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
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

// derefTime flattens the nullable timestamp a LEFT JOIN produces when a run has
// no score yet.
func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
