package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// versionsCacheTTL — how long we keep a GitHub releases response in
// memory before re-fetching. GitHub's unauthenticated rate limit is
// 60 req/h per IP; 5 minutes leaves plenty of head-room even with
// dozens of dashboard tabs polling concurrently.
const versionsCacheTTL = 5 * time.Minute

// versionsCacheLimit caps how many releases we return. GitHub's
// default per_page is 30; we ask for 100. Anything older typically
// isn't deploy-relevant for an operator picking the next rollout.
const versionsCacheLimit = 100

// versionsCache is the per-server in-memory cache for GitHub-releases
// lookups. Keyed by `<owner>/<repo>` so multiple services pointing at
// the same repo share one fetch. The mutex covers both reads and
// writes; the underlying releases slice is otherwise immutable.
type versionsCache struct {
	mu    sync.Mutex
	store map[string]versionsCacheEntry
}

type versionsCacheEntry struct {
	versions []string
	expires  time.Time
}

var sharedVersionsCache = &versionsCache{store: make(map[string]versionsCacheEntry)}

// get returns the cached versions for repo if still fresh. Second
// return is false on miss / expiry; caller refetches.
func (c *versionsCache) get(repo string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.store[repo]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.versions, true
}

func (c *versionsCache) put(repo string, versions []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[repo] = versionsCacheEntry{versions: versions, expires: time.Now().Add(versionsCacheTTL)}
}

// handleServiceVersions serves GET /services/{name}/versions. Returns
// the list of deploy-target version tags pulled from GitHub Releases
// for the configured repo (cfg.Artifact.GitHubRepo). Empty list when:
//
//   - the service's artifact URL doesn't reference ${VERSION} or
//     points at a local/file scheme (dev mode);
//   - cfg.Artifact.GitHubRepo is unset;
//   - the GitHub fetch fails (the dashboard falls back to versions
//     derived from deploy history + alloc.Version in that case).
//
// Results are cached for versionsCacheTTL per repo so a busy dashboard
// doesn't burn through the unauthenticated 60 req/h limit.
func (api *API) handleServiceVersions(w http.ResponseWriter, r *http.Request) {
	serviceName := r.PathValue("name")
	svc := api.findService(serviceName)
	if svc == nil {
		api.writeError(w, http.StatusNotFound, "service not found", nil)
		return
	}
	if !strings.Contains(svc.Artifact.URL, "${VERSION}") {
		api.writeJSON(w, http.StatusOK, map[string]any{"versions": []string{}})
		return
	}
	repo := api.ctx.Config().Artifact.GitHubRepo
	if repo == "" {
		api.writeJSON(w, http.StatusOK, map[string]any{"versions": []string{}})
		return
	}
	versions, err := fetchGitHubVersions(r.Context(), repo)
	if err != nil {
		api.writeJSON(w, http.StatusOK, map[string]any{"versions": []string{}, "error": err.Error()})
		return
	}
	api.writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// fetchGitHubVersions returns the cached release tags for repo, or
// hits the GitHub API when the cache is empty / stale. Network
// failures and non-2xx responses surface as errors so the handler can
// log them; the operator-visible list stays empty in that case.
func fetchGitHubVersions(ctx context.Context, repo string) ([]string, error) {
	if versions, ok := sharedVersionsCache.get(repo); ok {
		return versions, nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", repo, versionsCacheLimit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: %s", resp.Status)
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(releases))
	for _, r := range releases {
		if r.TagName != "" {
			versions = append(versions, r.TagName)
		}
	}
	sharedVersionsCache.put(repo, versions)
	return versions, nil
}
