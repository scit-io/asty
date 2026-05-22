package types

// ServiceVersion is the per-service pinned version maintained by the
// deployer and consumed by the scheduler when creating new allocations.
//
// Current is the version new allocations are spawned with (and what's
// being rolled out if a deploy is in flight). Previous is the rollback
// target: deploy.Begin reads it as plan.CurrentVersion, deploy success
// clears it, deploy.Revert restores it as Current.
//
// Persisted in KV under `service.<name>.version`. Absent record (zero
// value, both fields empty) means the service has never been deployed —
// scheduler falls back to "latest" in that case so dev URLs like
// `local` still work without an explicit deploy step.
type ServiceVersion struct {
	Current  string `json:"current,omitempty"`
	Previous string `json:"previous,omitempty"`
}
