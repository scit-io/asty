package types

// HealthState reports the latest probe result for an allocation.
// Defined as a typed string so the compiler catches stray literals;
// the underlying values preserve the JSON wire format that
// Allocation.HealthStatus and the health checker have always used.
type HealthState string

const (
	// HealthUnknown — no probe registered, or no result yet.
	HealthUnknown HealthState = ""

	// HealthHealthy — last probe succeeded.
	HealthHealthy HealthState = "healthy"

	// HealthUnhealthy — last probe failed (HTTP non-2xx, NATS no-reply,
	// timeout, etc.).
	HealthUnhealthy HealthState = "unhealthy"
)
