package codec

import (
	"encoding/json"

	"github.com/rs/zerolog/log"
)

// Marshal encodes v with the package's wire format. Drop-in
// replacement for encoding/json.Marshal — every internal asty NATS
// payload and KV record goes through this single seam so the format
// can be swapped for the whole system in one place.
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal decodes data into the value pointed to by v. Drop-in
// replacement for encoding/json.Unmarshal.
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// MustMarshal is the panic-or-empty variant for hot paths where the
// call site must stay one-line and the marshalling is "should never
// fail". On error it logs and returns []byte("{}") so NATS publishers
// and KV writers never see a nil slice they would have to
// special-case. With the json backend that byte literal is also valid
// JSON; a future binary backend would change this to the codec's
// own empty-object encoding.
func MustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("codec: marshal failed")
		return []byte("{}")
	}
	return b
}
