package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"asty/asty/internal/api/stream"
	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
	"asty/asty/internal/ops/deployer"
	"asty/asty/internal/ops/scheduler"
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

// handleServiceScale serves POST /services/{name}/scale and sets the
// per-service minimum copy count. Contract: scale = floor, not exact
// count — the autoscaler still grows above this in response to load.
// Leader-only is enforced by the leaderOnly middleware at registration.
//
// Rejects system-type services (409): they run exactly one per node
// by definition; the scheduler would re-create whatever the handler
// killed. Rejects count > MaxCopies (400) so the response isn't a
// silent lie — the autoscaler ceiling caps the effective live count
// at MaxCopies anyway.
//
// Scale-down stops victims via scheduler.PickRemovalVictims (DC-aware,
// shared with the autoscaler). Scale-up is realised by ReconcileService
// enqueueing the controller; the response reports the post-stop live
// count so the operator sees the real state, not the snapshot taken
// before the stops ran.
func (api *API) handleServiceScale(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
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
	svc := api.findService(serviceName)
	if svc == nil {
		api.writeError(w, http.StatusNotFound, "service not found", nil)
		return
	}
	if svc.Type == types.ServiceTypeSystem {
		api.writeError(w, http.StatusConflict,
			"cannot scale a system-type service; system services run one copy per node by definition", nil)
		return
	}
	if cap := api.ctx.Config().Autoscale.MaxCopies; cap > 0 && req.Count > cap {
		api.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("count %d exceeds autoscaler max_copies %d", req.Count, cap), nil)
		return
	}
	if err := api.ctx.ClusterState().SetServiceScale(serviceName, req.Count); err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to persist scale", err)
		return
	}
	allocs, _ := api.ctx.ClusterState().ListAllocations(serviceName)
	live := scheduler.LiveAllocations(allocs)
	before := len(live)
	if req.Count < before {
		nodes, _ := api.ctx.ClusterState().ListNodes()
		victims := scheduler.PickRemovalVictims(live, nodes, before-req.Count)
		for _, v := range victims {
			if !api.stopAndDeleteAllocation(r.Context(), w, serviceName, v.NodeID) {
				return
			}
		}
	}
	api.ctx.ReconcileService(serviceName)
	api.recordManualScaleEvent(serviceName, before, req.Count)
	postAllocs, _ := api.ctx.ClusterState().ListAllocations(serviceName)
	api.writeJSON(w, http.StatusOK, map[string]any{
		"service": serviceName,
		"desired": req.Count,
		"current": len(scheduler.LiveAllocations(postAllocs)),
	})
}

// recordManualScaleEvent broadcasts a scaling-event entry on NATS so
// manual scale shows up in the dashboard's "Scaling events" table on
// every node, not just the leader that handled the POST. FromCount is
// the live count before the operation; ToCount is the operator's
// target (req.Count). The action follows the sign of the delta —
// same vocabulary the autoscaler uses, so the UI's badge logic works
// unchanged. NodeID is empty because manual scale is service-scoped:
// scale-down may stop multiple copies and scale-up delegates
// placement to the scheduler. Reason is prefixed "manual:" so
// operators can filter trigger sources at a glance.
func (api *API) recordManualScaleEvent(service string, from, to int) {
	if from == to {
		return
	}
	action := types.ScaleUp
	if to < from {
		action = types.ScaleDown
	}
	api.ctx.MetricsStore().PublishEvent(autometrics.ScalingEvent{
		Service:   service,
		Action:    action,
		Reason:    fmt.Sprintf("manual: floor set to %d (was %d)", to, from),
		FromCount: from,
		ToCount:   to,
	})
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
