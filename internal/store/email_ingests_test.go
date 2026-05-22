package store

import "testing"

func TestInsertEmailIngest_Minimum(t *testing.T) {
	s := newStore(t)

	id, err := s.InsertEmailIngest(ctx, EmailIngestPayload{
		FromAddress: "devrim@example.com",
		Status:      "no_user",
		DKIMPass:    true,
	})
	if err != nil {
		t.Fatalf("InsertEmailIngest: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}
}

func TestInsertEmailIngest_FullFields(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{GitHubLogin: "alice"})

	msgID := "<abc@example.com>"
	id, err := s.InsertEmailIngest(ctx, EmailIngestPayload{
		MessageID:     &msgID,
		FromAddress:   "alice@example.com",
		Subject:       "Your booking",
		DKIMPass:      true,
		UserID:        &u.ID,
		Status:        "accepted",
		FlightsAdded:  2,
		FlightsFailed: 1,
		Error:         "",
	})
	if err != nil {
		t.Fatalf("InsertEmailIngest: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	// Verify the row landed with the expected user link.
	var gotUser *int64
	var gotAdded, gotFailed int
	if err := s.pool.QueryRow(ctx,
		`SELECT user_id, flights_added, flights_failed FROM email_ingests WHERE id = $1`, id,
	).Scan(&gotUser, &gotAdded, &gotFailed); err != nil {
		t.Fatalf("verify row: %v", err)
	}
	if gotUser == nil || *gotUser != u.ID {
		t.Errorf("user_id = %v, want %d", gotUser, u.ID)
	}
	if gotAdded != 2 || gotFailed != 1 {
		t.Errorf("added/failed = %d/%d, want 2/1", gotAdded, gotFailed)
	}
}




