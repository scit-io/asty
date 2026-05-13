package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"asty/asty/internal/core/types"
)

// handleServices returns loaded service definitions.
func (api *API) handleServices(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
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

	serviceName, action, _ := strings.Cut(path, "/")

	if action != "" {
		if !methodGuard(w, r, http.MethodPost) {
			return
		}

		if action == "scale" {
			api.handleServiceScale(w, r, serviceName)
			return
		}
		api.writeError(w, http.StatusNotFound, "unknown action", nil)
		return
	}

	if !methodGuard(w, r, http.MethodGet) {
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

// handleServiceScale sets the desired copy count for a service. Leader-only
// because it serializes against autoscaler decisions. Scale-up is reflected
// on next reconcile (triggered immediately via ReconcileService); scale-down
// is applied here: stops and deletes excess allocations in a stable order
// (sorted by node ID for determinism — operator can drain a specific node
// later if they need finer placement control).
func (api *API) handleServiceScale(w http.ResponseWriter, r *http.Request, serviceName string) {
	if !api.ctx.LeaderElection().IsLeader() {
		leaderInfo, _ := api.ctx.LeaderElection().GetLeader()
		api.writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("not leader, current leader: %s", leaderInfo.ID), nil)
		return
	}

	var req struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	if req.Count < 0 {
		api.writeError(w, http.StatusBadRequest, "count must be >= 0", nil)
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

	if err := api.ctx.ClusterState().SetServiceScale(serviceName, req.Count); err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to persist scale", err)
		return
	}

	allocs, _ := api.ctx.ClusterState().ListAllocations(serviceName)
	live := make([]*types.ServiceAllocation, 0, len(allocs))
	for _, a := range allocs {
		if a.Status.IsLive() {
			live = append(live, a)
		}
	}

	if req.Count < len(live) {
		victims := pickScaleDownVictims(live, len(live)-req.Count)
		for _, v := range victims {
			if err := api.ctx.StopServiceOnNode(v.NodeID, serviceName); err != nil {
				api.writeError(w, http.StatusInternalServerError, "stop dispatch failed", err)
				return
			}
			if err := api.ctx.ClusterState().DeleteAllocation(serviceName, v.NodeID); err != nil {
				api.writeError(w, http.StatusInternalServerError, "delete allocation failed", err)
				return
			}
		}
	}

	api.ctx.ReconcileService(serviceName)
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"service":     serviceName,
		"desired":     req.Count,
		"current":     len(live),
	})
}

// pickScaleDownVictims returns the N allocations to remove on scale-down.
// Sorted by NodeID (descending) for deterministic operator-visible behaviour.
func pickScaleDownVictims(live []*types.ServiceAllocation, n int) []*types.ServiceAllocation {
	if n <= 0 {
		return nil
	}
	if n >= len(live) {
		return live
	}
	sorted := append([]*types.ServiceAllocation(nil), live...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].NodeID > sorted[j].NodeID })
	return sorted[:n]
}

// handleDeploy initiates a deployment.
func (api *API) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodPost) {
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
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	history := api.ctx.Deployer().GetHistory()

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"deployments": history,
		"count":       len(history),
	})
}
