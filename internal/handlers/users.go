package handlers

import (
	"errors"
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Store.ListUsers(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.UserDTO, 0, len(users))
	for _, u := range users {
		out = append(out, api.ToUserDTO(u))
	}
	writeJSON(w, http.StatusOK, out)
}

type inviteReq struct {
	Username    string `json:"username"`
	Name        string `json:"name"`
	IsSuperuser bool   `json:"is_superuser"`
}

func (a *API) inviteUser(w http.ResponseWriter, r *http.Request) {
	var in inviteReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := a.Store.InviteUser(r.Context(), store.InvitePayload{
		Username:    in.Username,
		Name:        in.Name,
		IsSuperuser: in.IsSuperuser,
	})
	switch {
	case errors.Is(err, store.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username already registered")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, api.ToUserDTO(u))
}

type updateUserReq struct {
	Name        *string `json:"name,omitempty"`
	IsSuperuser *bool   `json:"is_superuser,omitempty"`
	IsActive    *bool   `json:"is_active,omitempty"`
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in updateUserReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Guard: a superuser cannot demote or deactivate themselves; that would
	// be easy to do by accident and lock everyone out.
	me := auth.UserFrom(r.Context())
	if me != nil && me.ID == id {
		if in.IsSuperuser != nil && !*in.IsSuperuser {
			writeError(w, http.StatusBadRequest, "cannot remove superuser from yourself")
			return
		}
		if in.IsActive != nil && !*in.IsActive {
			writeError(w, http.StatusBadRequest, "cannot deactivate yourself")
			return
		}
	}
	u, err := a.Store.UpdateUser(r.Context(), id, store.UpdateUserPayload{
		Name:        in.Name,
		IsSuperuser: in.IsSuperuser,
		IsActive:    in.IsActive,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ToUserDTO(u))
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if me != nil && me.ID == id {
		writeError(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	if err := a.Store.DeleteUser(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
