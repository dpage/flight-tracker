package store

import "context"

// Tag is one trip_tags row: a normalized matching key plus the display label
// as first typed. Tags group trips but never grant visibility.
type Tag struct {
	TripID       int64
	LabelNorm    string
	LabelDisplay string
}

// TagsByTrip returns the display labels set on a trip.
func (s *Store) TagsByTrip(ctx context.Context, tripID int64) ([]string, error) {
	return nil, ErrNotImplemented
}

// SetTripTags replaces the trip's tag set with the given display labels
// (normalizing each for the matching key).
func (s *Store) SetTripTags(ctx context.Context, tripID int64, labels []string) error {
	return ErrNotImplemented
}

// SuggestTags autocompletes over tags on trips the viewer can see, matching
// the normalized prefix q. Returns distinct display labels.
func (s *Store) SuggestTags(ctx context.Context, viewerID int64, q string) ([]string, error) {
	return nil, ErrNotImplemented
}
