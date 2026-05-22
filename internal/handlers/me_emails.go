package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/auth"
	"github.com/dpage/flight-tracker/internal/store"
)

// emailIngestDisabledMsg is the error body returned by /api/me/emails
// endpoints when email ingest is turned off.
const emailIngestDisabledMsg = "email ingest is disabled"

func (a *API) listMyEmails(w http.ResponseWriter, r *http.Request) {
	if a.Config == nil || !a.Config.EmailIngestEnabled {
		writeError(w, http.StatusServiceUnavailable, emailIngestDisabledMsg)
		return
	}
	u := auth.UserFrom(r.Context())
	emails, err := a.Store.EmailsByUser(r.Context(), u.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.UserEmailDTO, 0, len(emails))
	for _, e := range emails {
		out = append(out, api.ToUserEmailDTO(e))
	}
	writeJSON(w, http.StatusOK, out)
}

type addEmailReq struct {
	Address string `json:"address"`
}

func (a *API) addMyEmail(w http.ResponseWriter, r *http.Request) {
	if a.Config == nil || !a.Config.EmailIngestEnabled {
		writeError(w, http.StatusServiceUnavailable, emailIngestDisabledMsg)
		return
	}
	var in addEmailReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(in.Address) == "" {
		writeError(w, http.StatusBadRequest, "address required")
		return
	}
	u := auth.UserFrom(r.Context())
	row, token, err := a.Store.InsertUnverifiedEmail(r.Context(), u.ID, in.Address)
	switch {
	case errors.Is(err, store.ErrAddressTaken):
		writeError(w, http.StatusConflict, "address already registered")
		return
	case err != nil:
		handleStoreErr(w, err)
		return
	}
	if err := a.SendVerifyEmail(r.Context(), row.Address, token); err != nil {
		// Drop the just-inserted row so the user can re-try cleanly.
		_ = a.Store.DeleteUserEmail(r.Context(), u.ID, row.ID)
		writeError(w, http.StatusBadGateway, "could not send verification email")
		return
	}
	writeJSON(w, http.StatusCreated, api.ToUserEmailDTO(row))
}
