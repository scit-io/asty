package dashboard

import (
	"net/http"
	"time"

	"asty/asty/internal/core/types"
)

// handleServiceAutoscaler serves GET /services/{name}/autoscaler —
// current copies, thresholds, cooldowns, last action, and recent
// scaling events for this service.
//
// min_copies reflects the *effective* floor: operator-set scale
// override (kv.GetServiceScale) when present, falling back to
// cfg.Autoscale.MinCopies otherwise. Without this fold the dashboard
// would always show the cluster-wide default and silently hide the
// fact that a manual scale was applied. min_copies_default is the
// underlying cluster default so the UI can show "overridden" cues.
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
	deployInProgress := false
	if cd, err := api.ctx.ClusterState().GetServiceCooldown(serviceName); err == nil {
		cooldown = cd.Status(now, cfg.Autoscale.CooldownUp, cfg.Autoscale.CooldownDown)
		deployInProgress = cd.DeployInProgress
	}
	minCopies := cfg.Autoscale.MinCopies
	overridden := false
	if override, ok := api.ctx.ClusterState().GetServiceScale(serviceName); ok {
		minCopies = override
		overridden = true
	}
	events := api.ctx.MetricsStore().GetEvents(serviceName, 100)

	api.writeJSON(w, http.StatusOK, map[string]any{
		"service":              serviceName,
		"current_copies":       running,
		"min_copies":           minCopies,
		"min_copies_default":   cfg.Autoscale.MinCopies,
		"min_copies_override":  overridden,
		"max_copies":           cfg.Autoscale.MaxCopies,
		"target_cpu":           cfg.Autoscale.TargetCPU,
		"target_memory":        cfg.Autoscale.TargetMemory,
		"traffic_threshold":    cfg.Autoscale.TrafficRPSThreshold,
		"cooldown_up_active":   cooldown.UpActive,
		"cooldown_down_active": cooldown.DownActive,
		"deploy_in_progress":   deployInProgress,
		"last_action":          cooldown.LastAction,
		"last_action_at":       cooldown.LastActionAt,
		"events":               events,
	})
}
