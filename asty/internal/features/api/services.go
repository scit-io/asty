package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"asty/asty/internal/core/types"
)

// handleServices serves GET /services — list every loaded service
// definition. SSE flavour streams the same list on each tick.
func (api *API) handleServices(w http.ResponseWriter, r *http.Request) {
	if wantsSSE(r) {
		api.streamServices(w, r)
		return
	}
	api.writeJSON(w, http.StatusOK, map[string]any{
		"services": api.ctx.Services(),
		"count":    len(api.ctx.Services()),
	})
}

// handleService serves GET /services/{name} — single service detail
// (definition + current allocations). SSE flavour streams the same
// view + per-service metrics on each tick.
func (api *API) handleService(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
	if wantsSSE(r) {
		api.streamService(w, r, serviceName)
		return
	}
	service := api.findService(serviceName)
	if service == nil {
		api.writeError(w, http.StatusNotFound, "service not found", nil)
		return
	}
	allocs, _ := api.ctx.ClusterState().ListAllocations(serviceName)
	api.writeJSON(w, http.StatusOK, map[string]any{
		"service":     service,
		"allocations": allocs,
	})
}

// handleServiceAllocations serves GET /services/{name}/allocations.
func (api *API) handleServiceAllocations(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
	if api.findService(serviceName) == nil {
		api.writeError(w, http.StatusNotFound, "service not found", nil)
		return
	}
	allocs, err := api.ctx.ClusterState().ListAllocations(serviceName)
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to list allocations", err)
		return
	}
	api.writeJSON(w, http.StatusOK, map[string]any{
		"service":     serviceName,
		"allocations": allocs,
		"count":       len(allocs),
	})
}

// findService returns the loaded ServiceDefinition for name, or nil.
// Linear scan — service lists are short (<20) in practice.
func (api *API) findService(name string) *types.ServiceDefinition {
	for _, svc := range api.ctx.Services() {
		if svc.Name == name {
			return svc
		}
	}
	return nil
}

// handleServiceScale serves POST /services/{name}/scale. Leader-only
// because it serialises against autoscaler decisions. Scale-up is
// reflected on next reconcile (triggered immediately via
// ReconcileService); scale-down is applied here: stops and deletes
// excess allocations in a stable order (sorted by node ID for
// determinism — operator can drain a specific node later if they
// need finer placement control).
func (api *API) handleServiceScale(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
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
	if api.findService(serviceName) == nil {
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
	api.writeJSON(w, http.StatusOK, map[string]any{
		"service": serviceName,
		"desired": req.Count,
		"current": len(live),
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

// handleServiceDeploy serves POST /services/{name}/deploy. Body
// `{"version": "..."}` — the service name comes from the URL.
// Leader-only.
func (api *API) handleServiceDeploy(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	if req.Version == "" {
		api.writeError(w, http.StatusBadRequest, "version required", nil)
		return
	}
	if !api.ctx.LeaderElection().IsLeader() {
		leaderInfo, _ := api.ctx.LeaderElection().GetLeader()
		api.writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("not leader, current leader: %s", leaderInfo.ID), nil)
		return
	}
	status, err := api.ctx.DeployService(r.Context(), serviceName, req.Version)
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "deployment failed", err)
		return
	}
	api.writeJSON(w, http.StatusOK, status)
}

// handleServiceDeployHistory serves GET /services/{name}/deploy — past
// deploy records for this service.
func (api *API) handleServiceDeployHistory(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
	all := api.ctx.Deployer().GetHistory()
	filtered := make([]any, 0)
	for _, rec := range all {
		if rec.Service == serviceName {
			filtered = append(filtered, rec)
		}
	}
	api.writeJSON(w, http.StatusOK, map[string]any{
		"service":     serviceName,
		"deployments": filtered,
		"count":       len(filtered),
	})
}
