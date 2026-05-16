// Package codec is the seam for serialization formats used on asty's
// internal wires and storage. Two variables expose two independent
// codecs:
//
//   - Wire — agent RPC payloads and replies, gateway metrics reports,
//     any other ephemeral NATS message that is never read by an
//     operator. Target: small and fast representation.
//
//   - State — NATS JetStream KV records (nodes, allocations,
//     cooldowns, scale, leader Info). Target: human-readable so
//     `nats kv get` stays useful during incident response.
//
// Both default to encoding/json. To switch Wire to a binary format
// (CBOR, MessagePack, etc.) replace `codec.Wire`; State stays JSON
// unless its variable is replaced too.
//
// Browser-bound surfaces (SSE frames, HTTP API responses, drain
// progress passthrough) and foreign JSON (NATS /varz, /jsz) are
// outside this package's reach — their format is dictated by the
// consumer and they keep using encoding/json directly.
package codec
