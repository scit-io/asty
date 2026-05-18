package rest

import (
	"net/http"
	"time"

	"asty/asty/internal/core/types"
)

// handleServiceAutoscaler serves GET /services/{name}/autoscaler —
// current copies, thresholds, cooldowns, last action, and recent
// scaling events for this service.
func (api *API) handleServiceAutoscaler(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
	cfg := api.ctx.Config()
	now := time.Now()

	allocs, _ := api.ctx.ClusterState().ListAllocations(serviceName)
	running := 0
	for _, a := range allocs {
		if a.Status == types.AllocRunning {
			running++
		}
	}
	var cooldown types.CooldownStatus
	if cd, err := api.ctx.ClusterState().GetServiceCooldown(serviceName); err == nil {
		cooldown = cd.Status(now, cfg.Autoscale.CooldownUp, cfg.Autoscale.CooldownDown)
	}
	events := api.ctx.MetricsStore().GetEvents(serviceName, 100)

	api.writeJSON(w, http.StatusOK, map[string]any{
		"service":              serviceName,
		"current_copies":       running,
		"min_copies":           cfg.Autoscale.MinCopies,
		"target_cpu":           cfg.Autoscale.TargetCPU,
		"target_memory":        cfg.Autoscale.TargetMemory,
		"traffic_threshold":    cfg.Autoscale.TrafficRPSThreshold,
		"cooldown_up_active":   cooldown.UpActive,
		"cooldown_down_active": cooldown.DownActive,
		"last_action":          cooldown.LastAction,
		"last_action_at":       cooldown.LastActionAt,
		"events":               events,
	})
}
