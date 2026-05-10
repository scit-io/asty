package api

import "net/http"

// methodGuard reports whether r.Method matches any of the allowed verbs.
// On mismatch it writes a 405 Method Not Allowed response and returns
// false, so handlers can use:
//
//	if !methodGuard(w, r, http.MethodGet) { return }
//
// instead of repeating the same four-line boilerplate at the top of
// every handler.
func methodGuard(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	return false
}
