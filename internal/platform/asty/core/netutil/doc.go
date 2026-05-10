// Package netutil holds small networking helpers shared by agent and server:
// resolving the local hostname/IP, connecting to NATS with consistent
// options, and waiting for KV buckets to become usable.
//
// Anything in this package is intentionally generic — it has no knowledge of
// Asty's domain types so it can be reused by both the agent and server
// processes without creating circular dependencies.
package netutil
