package api

import (
	"fmt"
	"net/http"
	"time"

	"asty/internal/platform/asty/core/types"
)

// handleAutoscalerEvents returns autoscaler scaling events.
func (api *API) handleAutoscalerEvents(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	service := r.URL.Query().Get("service")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	events := api.ctx.MetricsStore().GetEvents(service, limit)

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// handleAutoscalerStatus returns current autoscaler state per service.
func (api *API) handleAutoscalerStatus(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	cfg := api.ctx.Config()
	servicesStatus := make(map[string]interface{})
	now := time.Now()

	for _, svc := range api.ctx.Services() {
		allocs, _ := api.ctx.ClusterState().ListAllocations(svc.Name)
		running := 0
		for _, a := range allocs {
			if a.Status == "running" {
				running++
			}
		}

		var cooldown types.CooldownStatus
		if cd, err := api.ctx.ClusterState().GetServiceCooldown(svc.Name); err == nil {
			cooldown = cd.Status(now, cfg.CooldownUp, cfg.CooldownDown)
		}

		servicesStatus[svc.Name] = map[string]interface{}{
			"current_copies":       running,
			"min_copies":           cfg.MinCopies,
			"target_cpu":           cfg.TargetCPU,
			"target_memory":        cfg.TargetMemory,
			"traffic_threshold":    cfg.TrafficRPSThreshold,
			"cooldown_up_active":   cooldown.UpActive,
			"cooldown_down_active": cooldown.DownActive,
			"last_action":          cooldown.LastAction,
			"last_action_at":       cooldown.LastActionAt,
		}
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": servicesStatus,
	})
}

// handleEvents returns recent cluster events from the ring buffer.
func (api *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	events := api.ctx.EventBuffer().GetLast(200)
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}
