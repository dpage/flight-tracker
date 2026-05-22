package store

import "context"

// EmailIngestPayload is the input for InsertEmailIngest.
type EmailIngestPayload struct {
	MessageID     *string
	FromAddress   string
	Subject       string
	DKIMPass      bool
	UserID        *int64
	Status        string
	FlightsAdded  int
	FlightsFailed int
	Error         string
}

// InsertEmailIngest records the outcome of processing one inbound email.
// Returns the new row's id.
func (s *Store) InsertEmailIngest(ctx context.Context, in EmailIngestPayload) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO email_ingests
			(message_id, from_address, subject, dkim_pass, user_id, status,
			 flights_added, flights_failed, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		in.MessageID, in.FromAddress, in.Subject, in.DKIMPass, in.UserID,
		in.Status, in.FlightsAdded, in.FlightsFailed, in.Error,
	).Scan(&id)
	return id, err
}
