package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/store"
)

// friendEmailSendTimeout bounds each outbound sendmail invocation so a
// stalled MTA can't pin the request worker. The handler still waits for
// the send before responding (to keep delivery best-effort synchronous),
// but a hung send aborts at this deadline and the request returns.
const friendEmailSendTimeout = 5 * time.Second

// inviteFriendAcceptedBody is the response every successful POST to
// /api/friends/invite returns, regardless of whether the email matched a
// verified user, queued a pending sign-up invite, or self-matched. Keeping
// the body byte-identical across the three paths is the no-enumeration
// guarantee — see TestInviteFriendResponseIdenticalForKnownAndUnknown.
const inviteFriendAcceptedBody = `{"status":"accepted"}` + "\n"

func (a *API) listFriends(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	rows, err := a.Store.ListFriendships(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FriendshipDTO, 0, len(rows))
	for _, f := range rows {
		out = append(out, api.ToFriendshipDTO(f, me.ID))
	}
	writeJSON(w, http.StatusOK, out)
}

type inviteFriendReq struct {
	Email   string `json:"email"`
	Message string `json:"message,omitempty"`
}

func (a *API) inviteFriend(w http.ResponseWriter, r *http.Request) {
	var in inviteFriendReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	addr := strings.TrimSpace(in.Email)
	if addr == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}
	addr = parsed.Address

	me := auth.UserFrom(r.Context())

	// Hide whether the target email exists or not — both branches return
	// the same response. We still do the work synchronously so the email
	// hits Postfix's queue before the response (callers that retry on
	// failure get a useful answer).
	target, err := a.Store.UserByVerifiedEmail(r.Context(), addr)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.inviteFriendByEmail(r.Context(), me, addr, in.Message)
	case err != nil:
		handleStoreErr(w, err)
		return
	default:
		// If they're trying to friend their own verified address, treat as
		// success — no edge to create, but we don't expose the self-match.
		if target.ID != me.ID {
			a.inviteFriendByUserID(r.Context(), me, target, in.Message)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(inviteFriendAcceptedBody))
}

// inviteFriendByUserID issues (or no-ops on) a pending friendship and
// emails the recipient. Errors are logged but never surfaced to the caller
// — see inviteFriend's enumeration-leak comment.
func (a *API) inviteFriendByUserID(ctx context.Context, me, target *store.User, message string) {
	friendship, err := a.Store.RequestFriendship(ctx, me.ID, target.ID)
	if err != nil {
		slog.Error("friend invite: request failed", "err", err, "from", me.ID, "to", target.ID)
		return
	}
	// Only notify on a brand-new pending request; duplicate calls or
	// already-accepted friendships stay silent.
	if friendship.Status != "pending" || friendship.RequestedBy != me.ID {
		return
	}
	a.sendFriendRequestNotification(ctx, me, target, message)
}

// inviteFriendByEmail queues a pending invite for an address that doesn't
// have an account yet and emails the recipient. Suppresses the email when
// the same inviter has already queued the same address (duplicate clicks).
func (a *API) inviteFriendByEmail(ctx context.Context, me *store.User, addr, message string) {
	created, err := a.Store.UpsertPendingFriendInvite(ctx, me.ID, addr, message)
	if err != nil {
		slog.Error("friend invite: upsert pending failed", "err", err, "from", me.ID)
		return
	}
	if !created {
		return
	}
	a.sendFriendInviteEmail(ctx, me, addr, message)
}

func (a *API) sendFriendRequestNotification(ctx context.Context, inviter, recipient *store.User, message string) {
	if a.Config == nil || a.Config.MailFromAddress == "" {
		slog.Warn("friend invite: MAIL_FROM_ADDRESS unset, skipping notification email",
			"from", inviter.ID, "to", recipient.ID)
		return
	}
	addrs, err := a.Store.EmailsByUser(ctx, recipient.ID)
	if err != nil {
		slog.Error("friend invite: load recipient emails failed", "err", err, "to", recipient.ID)
		return
	}
	to := ""
	for _, e := range addrs {
		if e.Verified {
			to = e.Address
			break
		}
	}
	if to == "" {
		// Edge case: matched user has no verified email row anymore.
		return
	}
	msg := buildFriendRequestEmail(friendRequestInput{
		FromAddr:     a.Config.MailFromAddress,
		ToAddr:       to,
		PublicURL:    a.Config.PublicURL,
		InviterName:  inviter.Name,
		InviterLogin: inviter.Username,
		Message:      message,
	})
	sendCtx, cancel := context.WithTimeout(ctx, friendEmailSendTimeout)
	defer cancel()
	if err := mailer.Send(sendCtx, a.Config.SendmailPath, a.Config.MailFromAddress, msg); err != nil {
		slog.Error("friend invite: send notification failed", "err", err)
	}
}

func (a *API) sendFriendInviteEmail(ctx context.Context, inviter *store.User, to, message string) {
	if a.Config == nil || a.Config.MailFromAddress == "" {
		slog.Warn("friend invite: MAIL_FROM_ADDRESS unset, skipping invite email",
			"from", inviter.ID, "to", to)
		return
	}
	msg := buildFriendInviteEmail(friendInviteInput{
		FromAddr:     a.Config.MailFromAddress,
		ToAddr:       to,
		PublicURL:    a.Config.PublicURL,
		InviterName:  inviter.Name,
		InviterLogin: inviter.Username,
		Message:      message,
	})
	sendCtx, cancel := context.WithTimeout(ctx, friendEmailSendTimeout)
	defer cancel()
	if err := mailer.Send(sendCtx, a.Config.SendmailPath, a.Config.MailFromAddress, msg); err != nil {
		slog.Error("friend invite: send invite failed", "err", err)
	}
}

func (a *API) acceptFriend(w http.ResponseWriter, r *http.Request) {
	otherID, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad user id")
		return
	}
	me := auth.UserFrom(r.Context())
	f, err := a.Store.AcceptFriendship(r.Context(), me.ID, otherID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ToFriendshipDTO(f, me.ID))
}

func (a *API) removeFriend(w http.ResponseWriter, r *http.Request) {
	otherID, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad user id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.Store.RemoveFriendship(r.Context(), me.ID, otherID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

