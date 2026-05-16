package codec

import (
	"encoding/json"

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
// reports). Targets the smallest and fastest representation —
// nothing on this path is intended to be read by an operator.
// Switch to a binary backend by replacing this variable.
var Wire Codec = jsonCodec{}

// State is the codec for NATS JetStream KV records (cluster nodes,
// service allocations, cooldowns, scale overrides, leader Info).
// Optimised for human readability via `nats kv get` over wire size:
// even a binary Wire codec does not propagate here unless this
// variable is replaced too.
var State Codec = jsonCodec{}

// jsonCodec is the default encoding/json-backed Codec used by both
// Wire and State until a binary backend is introduced.
type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error)      { return json.Marshal(v) }
func (jsonCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// MustMarshal returns []byte("{}") after logging the error so NATS
// publishers and KV writers never see a nil slice they would have to
// special-case. A future binary backend would change the empty-object
// literal to its own encoding.
func (jsonCodec) MustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("codec: marshal failed")
		return []byte("{}")
	}
	return b
}
