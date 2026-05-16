// Package codec is the single seam for the serialization format
// used on internal asty wires that do not pass straight through to a
// browser or to a third party: agent RPC payloads and NATS JetStream
// KV state. Browser-bound surfaces (SSE frames, HTTP API responses,
// drain progress passthrough) keep using encoding/json directly
// because their format is dictated by the consumer. Foreign
// JSON (NATS /varz, /jsz) likewise stays on encoding/json.
//
// The default backend is encoding/json. To migrate the system to a
// binary format (CBOR, MessagePack, etc.) change the three functions
// in codec.go; every call site that already uses codec.Marshal /
// codec.Unmarshal / codec.MustMarshal moves with no further edits.
package codec
