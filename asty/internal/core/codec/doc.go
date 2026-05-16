// Package codec is the seam for serialization formats used on asty's
// internal wires and storage. Two variables expose two independent
// codecs:
//
//   - Wire — agent RPC payloads and replies, gateway metrics reports,
//     any other ephemeral NATS message.
//
//   - State — NATS JetStream KV records (nodes, allocations,
//     cooldowns, scale, leader Info).
//
// Both default to CBOR (fxamacker/cbor/v2) — production setting.
// When dev_mode=true is in effect (config.asty or A_DEV_MODE), main
// calls UseJSONForDev() to swap both back to JSON so every NATS
// subject is readable via `nats sub` and every KV record via
// `nats kv get` with no extra tooling. All nodes in one cluster
// must share the same mode.
//
// Browser-bound surfaces (SSE frames, HTTP API responses, drain
// progress passthrough) and foreign JSON (NATS /varz, /jsz) are
// outside this package's reach — their format is dictated by the
// consumer and they keep using encoding/json directly.
package codec
