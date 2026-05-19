package handlers

import "net/http"

// CapabilitiesDTO exposes server-side feature flags the frontend uses to
// decide which UI affordances to show — most notably whether a Resolver is
// wired up, which lets the Add Flight dialog drop to its minimal "ident +
// date" form.
type CapabilitiesDTO struct {
	ResolverAvailable bool `json:"resolver_available"`
}

func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	_ = r
	writeJSON(w, http.StatusOK, CapabilitiesDTO{
		ResolverAvailable: a.Config != nil && a.Config.ResolverAvailable(),
	})
}
