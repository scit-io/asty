package types

import "fmt"

// GatewayMetricsReport is the payload an agent's embedded gateway
// publishes every rpsReporterInterval on MetricsGatewaySubject.
// Server subscribes via MetricsGatewayPattern and feeds ValidRPS into
// the autoscaler's locality-aware scale-up trigger. Services is the
// same delta sliced per first-path-segment so SSE handlers can
// attribute traffic to individual allocations (one local queue-group
// per service on this node).
type GatewayMetricsReport struct {
	NodeID   string             `json:"node_id"`
	ValidRPS float64            `json:"valid_rps"`
	Services map[string]float64 `json:"services,omitempty"`
}

const (
	metricsGatewaySubjectFormat = "asty.v1.metrics.gateway.%s"
	metricsGatewayPattern       = "asty.v1.metrics.gateway.*"
)

// MetricsGatewaySubject returns the NATS subject a gateway on nodeID
// publishes its RPS sample to.
func MetricsGatewaySubject(nodeID string) string {
	return fmt.Sprintf(metricsGatewaySubjectFormat, nodeID)
}

// MetricsGatewayPattern returns the wildcard the server subscribes to
// in order to ingest reports from every node.
func MetricsGatewayPattern() string {
	return metricsGatewayPattern
}
