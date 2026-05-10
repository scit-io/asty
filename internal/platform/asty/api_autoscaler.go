package asty

import (
	"fmt"
	"net/http"
	"time"
)

// handleAutoscalerEvents returns autoscaler scaling events
func (api *API) handleAutoscalerEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	service := r.URL.Query().Get("service")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	events := api.server.metricsStore.GetEvents(service, limit)

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// handleAutoscalerStatus returns current autoscaler state per service
func (api *API) handleAutoscalerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	servicesStatus := make(map[string]interface{})

	for _, svc := range api.server.services {
		allocs, _ := api.server.clusterState.ListAllocations(svc.Name)
		running := 0
		for _, a := range allocs {
			if a.Status == "running" {
				running++
			}
		}

		cooldownUp := false
		cooldownDown := false
		var lastAction string
		var lastActionAt int64

		if cd, err := api.server.clusterState.GetServiceCooldown(svc.Name); err == nil {
			if !cd.LastScaleUp.IsZero() {
				if time.Since(cd.LastScaleUp) < api.server.cfg.CooldownUp {
					cooldownUp = true
				}
				lastAction = "scale_up"
				lastActionAt = cd.LastScaleUp.Unix()
			}
			if !cd.LastScaleDown.IsZero() {
				if time.Since(cd.LastScaleDown) < api.server.cfg.CooldownDown {
					cooldownDown = true
				}
				if cd.LastScaleDown.Unix() > lastActionAt {
					lastAction = "scale_down"
					lastActionAt = cd.LastScaleDown.Unix()
				}
			}
		}

		servicesStatus[svc.Name] = map[string]interface{}{
			"current_copies":       running,
			"min_copies":           api.server.cfg.MinCopies,
			"target_cpu":           api.server.cfg.TargetCPU,
			"target_memory":        api.server.cfg.TargetMemory,
			"traffic_threshold":    api.server.cfg.TrafficRPSThreshold,
			"cooldown_up_active":   cooldownUp,
			"cooldown_down_active": cooldownDown,
			"last_action":          lastAction,
			"last_action_at":       lastActionAt,
		}
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": servicesStatus,
	})
}

// handleEvents returns recent cluster events from the ring buffer.
func (api *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	events := api.server.eventBuffer.GetLast(200)
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}
