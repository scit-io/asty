package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"

	"asty/asty/internal/api/stream"
	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/deployer"
)

// handleServices serves GET /services — list every loaded service
// definition. SSE flavour streams the same list on each tick.
func (api *API) handleServices(w http.ResponseWriter, r *http.Request) {
	if transportSSE(r) {
		stream.Services(api.streamCtx, w, r)
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
	if transportSSE(r) {
		stream.Service(api.streamCtx, w, r, serviceName)
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

// handleServiceDeploy serves POST /services/{name}/deploy. Body
// `{"version": "..."}` — the service name comes from the URL.
// Leader-only is enforced by the leaderOnly middleware applied at
// route registration, not inline here.
//
// Deploy is async: returns 202 with the initial running status the
// moment the deploy goroutine is launched. The UI subscribes to the
// asty.v1.deploy.progress.<service> SSE stream for live progress —
// the browser does not wait on the multi-minute rollout. Concurrent
// deploys on the same service surface as 409 via deployer.ErrDeployInFlight.
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
	status, err := api.ctx.DeployService(r.Context(), serviceName, req.Version)
	if err != nil {
		if errors.Is(err, deployer.ErrDeployInFlight) {
			api.writeError(w, http.StatusConflict, err.Error(), nil)
			return
		}
		api.writeError(w, http.StatusInternalServerError, "deployment failed", err)
		return
	}
	api.writeJSON(w, http.StatusAccepted, status)
}

// handleServiceDeployHistory serves GET /services/{name}/deploy. The
// JSON flavour returns past deploy records (history). The SSE flavour
// — selected via `Accept: text/event-stream` — streams live deploy
// progress events for this service so the UI can replace its 10-second
// polling loop with an event-driven feed.
func (api *API) handleServiceDeployHistory(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
	if transportSSE(r) {
		stream.Deploy(api.streamCtx, w, r, serviceName)
		return
	}
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
