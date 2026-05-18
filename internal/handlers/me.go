package handlers

import (
	"net/http"

	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/auth"
)

func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	writeJSON(w, http.StatusOK, api.ToUserDTO(u))
}
