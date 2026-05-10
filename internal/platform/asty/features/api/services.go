package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"asty/internal/platform/asty/core/types"
)

// handleServices returns loaded service definitions.
func (api *API) handleServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": api.ctx.Services(),
		"count":    len(api.ctx.Services()),
	})
}

// handleServicesWithActions handles /api/v1/services/:name and /api/v1/services/:name/action.
func (api *API) handleServicesWithActions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v1/services/"):]
	if path == "" {
		api.handleServices(w, r)
		return
	}

	var serviceName, action string
	for i, ch := range path {
		if ch == '/' {
			serviceName = path[:i]
			action = path[i+1:]
			break
		}
	}
	if serviceName == "" {
		serviceName = path
	}

	if action != "" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if action == "scale" {
			var req struct {
				Count int `json:"count"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				api.writeError(w, http.StatusBadRequest, "invalid request body", err)
				return
			}

			api.writeJSON(w, http.StatusOK, map[string]interface{}{
				"service": serviceName,
				"count":   req.Count,
				"message": "scaling not yet implemented",
			})
			return
		}

		api.writeError(w, http.StatusNotFound, "unknown action", nil)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var service *types.ServiceDefinition
	for _, svc := range api.ctx.Services() {
		if svc.Name == serviceName {
			service = svc
			break
		}
	}

	if service == nil {
		api.writeError(w, http.StatusNotFound, "service not found", nil)
		return
	}

	allocs, _ := api.ctx.ClusterState().ListAllocations(serviceName)

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"service":     service,
		"allocations": allocs,
	})
}

// handleDeploy initiates a deployment.
func (api *API) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Service string `json:"service"`
		Version string `json:"version"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if req.Service == "" || req.Version == "" {
		api.writeError(w, http.StatusBadRequest, "service and version required", nil)
		return
	}

	if !api.ctx.LeaderElection().IsLeader() {
		leaderInfo, _ := api.ctx.LeaderElection().GetLeader()
		api.writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("not leader, current leader: %s", leaderInfo.ID), nil)
		return
	}

	status, err := api.ctx.DeployService(r.Context(), req.Service, req.Version)
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "deployment failed", err)
		return
	}

	api.writeJSON(w, http.StatusOK, status)
}

// handleDeployments returns deployment history.
func (api *API) handleDeployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	history := api.ctx.Deployer().GetHistory()

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"deployments": history,
		"count":       len(history),
	})
}
