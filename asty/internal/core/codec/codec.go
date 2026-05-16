package codec

import (
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog/log"
)

// Codec describes a serialization backend. Implementations must be
// safe for concurrent use.
type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
	MustMarshal(v any) []byte
}

// Wire is the codec for ephemeral inter-process message-passing on
// NATS subjects (agent RPC commands and replies, gateway metrics
// reports). Defaults to CBOR — both ends of every Wire path live in
// the same asty binary so there is no external compatibility
// concern, and CBOR is roughly half the size and ~3x faster than
// JSON on these payloads.
var Wire Codec = cborCodec{}

// State is the codec for NATS JetStream KV records (cluster nodes,
// service allocations, cooldowns, scale overrides, leader Info).
// Stays on JSON so `nats kv get` remains useful during incident
// response.
var State Codec = jsonCodec{}

// jsonCodec backs State and used to back Wire; still here so a
// future operator could pin Wire back to JSON for debugging by
// reassigning the variable.
type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error)      { return json.Marshal(v) }
func (jsonCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// MustMarshal returns []byte("{}") after logging the error so NATS
// publishers and KV writers never see a nil slice they would have to
// special-case. With the json backend that byte literal is also valid
// JSON.
func (jsonCodec) MustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("codec: json marshal failed")
		return []byte("{}")
	}
	return b
}

// cborCodec backs Wire. fxamacker/cbor/v2 default options are
// RFC 8949-conformant; struct field names are used verbatim (we
// control both ends, no need to match JSON tag spelling).
type cborCodec struct{}

func (cborCodec) Marshal(v any) ([]byte, error)      { return cbor.Marshal(v) }
func (cborCodec) Unmarshal(data []byte, v any) error { return cbor.Unmarshal(data, v) }

// emptyCBORMap is CBOR encoding for a definite-length zero-pair map
// (a single byte 0xa0 — the binary analogue of JSON "{}"). Used as
// the fail-soft sentinel so consumers never receive nil.
var emptyCBORMap = []byte{0xa0}

func (cborCodec) MustMarshal(v any) []byte {
	b, err := cbor.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("codec: cbor marshal failed")
		return emptyCBORMap
	}
	return b
}
