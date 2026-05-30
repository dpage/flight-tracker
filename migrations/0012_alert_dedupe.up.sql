-- Wave 2B: flight-alert dedupe (spec §9).
--
-- The poller's refresh path emits an alert when a flight part's status/time
-- crosses a meaningful threshold (cancellation, diversion, or a delay >= the
-- recipient's alert_prefs threshold). To stop the same change being re-sent on
-- every poll tick, we stamp the part with a signature of the last alerted
-- state. The next tick only alerts when the freshly-computed signature differs
-- from what's stored. Additive: a NULL column means "never alerted", so the
-- first meaningful change always fires.
ALTER TABLE flight_details ADD COLUMN last_alert_sig TEXT;
