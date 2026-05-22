package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/config"
)

func TestListMyEmails_Empty(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)

	w := e.req(t, "GET", "/api/me/emails", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeBody[[]api.UserEmailDTO](t, w)
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestListMyEmails_ShowsOwn(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	if _, _, err := e.store.InsertUnverifiedEmail(context.Background(), uid, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	w := e.req(t, "GET", "/api/me/emails", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeBody[[]api.UserEmailDTO](t, w)
	if len(got) != 1 || got[0].Address != "alice@example.com" {
		t.Errorf("got = %+v", got)
	}
	if got[0].Verified {
		t.Error("freshly inserted row should be unverified")
	}
}

func TestListMyEmails_HidesOthers(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	other := e.user(t, "bob", false)
	if _, _, err := e.store.InsertUnverifiedEmail(context.Background(), other, "bob@example.com"); err != nil {
		t.Fatal(err)
	}

	w := e.req(t, "GET", "/api/me/emails", nil, uid)
	got := decodeBody[[]api.UserEmailDTO](t, w)
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestMyEmails_RequiresAuth(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	if w := e.req(t, "GET", "/api/me/emails", nil, 0); w.Code != http.StatusUnauthorized {
		t.Errorf("anon GET = %d, want 401", w.Code)
	}
}

func TestAddMyEmail_HappyPath(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	var sent struct {
		to    string
		token string
	}
	e.api.SendVerifyEmail = func(_ context.Context, to, token string) error {
		sent.to, sent.token = to, token
		return nil
	}

	w := e.req(t, "POST", "/api/me/emails", map[string]string{"address": "alice@example.com"}, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeBody[api.UserEmailDTO](t, w)
	if got.Address != "alice@example.com" || got.Verified {
		t.Errorf("got = %+v", got)
	}
	if sent.to != "alice@example.com" || sent.token == "" {
		t.Errorf("sender called with to=%q token=%q", sent.to, sent.token)
	}
}

func TestAddMyEmail_AddressTaken(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	other := e.user(t, "bob", false)
	if _, _, err := e.store.InsertUnverifiedEmail(context.Background(), other, "shared@example.com"); err != nil {
		t.Fatal(err)
	}
	e.api.SendVerifyEmail = func(context.Context, string, string) error { return nil }

	w := e.req(t, "POST", "/api/me/emails", map[string]string{"address": "shared@example.com"}, uid)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestAddMyEmail_SendFailureRollsBackRow(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	e.api.SendVerifyEmail = func(context.Context, string, string) error {
		return errors.New("sendmail unavailable")
	}

	w := e.req(t, "POST", "/api/me/emails", map[string]string{"address": "alice@example.com"}, uid)
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
	emails, _ := e.store.EmailsByUser(context.Background(), uid)
	if len(emails) != 0 {
		t.Errorf("row left behind after send failure: %+v", emails)
	}
}

func TestAddMyEmail_EmptyAddress(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	e.api.SendVerifyEmail = func(context.Context, string, string) error { return nil }

	w := e.req(t, "POST", "/api/me/emails", map[string]string{"address": "   "}, uid)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAddMyEmail_DisabledReturns503(t *testing.T) {
	e := setup(t, nil, &config.Config{}) // EmailIngestEnabled = false
	uid := e.user(t, "alice", false)
	w := e.req(t, "POST", "/api/me/emails", map[string]string{"address": "x@example.com"}, uid)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestResendMyEmail_HappyPath(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	row, _, _ := e.store.InsertUnverifiedEmail(context.Background(), uid, "alice@example.com")

	var sentToken string
	e.api.SendVerifyEmail = func(_ context.Context, _, token string) error {
		sentToken = token
		return nil
	}

	w := e.req(t, "POST", "/api/me/emails/"+itoa(row.ID)+"/resend", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if sentToken == "" {
		t.Error("verification email not sent on resend")
	}
}

func TestResendMyEmail_AlreadyVerified(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	if err := e.store.UpsertVerifiedEmail(context.Background(), uid, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	rows, _ := e.store.EmailsByUser(context.Background(), uid)
	e.api.SendVerifyEmail = func(context.Context, string, string) error { return nil }

	w := e.req(t, "POST", "/api/me/emails/"+itoa(rows[0].ID)+"/resend", nil, uid)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestResendMyEmail_NotMine(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	other := e.user(t, "bob", false)
	row, _, _ := e.store.InsertUnverifiedEmail(context.Background(), other, "bob@example.com")
	e.api.SendVerifyEmail = func(context.Context, string, string) error { return nil }

	w := e.req(t, "POST", "/api/me/emails/"+itoa(row.ID)+"/resend", nil, uid)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestDeleteMyEmail_HappyPath(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	row, _, _ := e.store.InsertUnverifiedEmail(context.Background(), uid, "alice@example.com")

	w := e.req(t, "DELETE", "/api/me/emails/"+itoa(row.ID), nil, uid)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	got, _ := e.store.EmailsByUser(context.Background(), uid)
	if len(got) != 0 {
		t.Errorf("row not deleted: %+v", got)
	}
}

func TestDeleteMyEmail_NotMine(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	other := e.user(t, "bob", false)
	row, _, _ := e.store.InsertUnverifiedEmail(context.Background(), other, "bob@example.com")

	w := e.req(t, "DELETE", "/api/me/emails/"+itoa(row.ID), nil, uid)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
