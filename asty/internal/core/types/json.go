package types

import (
	"encoding/json"

	"github.com/rs/zerolog/log"
)

// MustJSON marshals v to JSON. On error it logs and returns "{}" so that
// streaming endpoints (SSE, NATS broadcasts) never see a nil byte slice
// they would have to special-case. Domain types in this package are all
// JSON-marshalable, so the error branch is "should never happen" and is
// only there to keep the call site short.
func MustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("MustJSON: marshal failed")
		return []byte("{}")
	}
	return b
}
