-- Reverse 0010. The legacy flights / flight_passengers / flight_shares tables
-- were never dropped by the up migration, so the down is a clean drop of the
-- new objects plus restoring the positions → flights foreign key.

DROP TRIGGER IF EXISTS plan_passengers_ensure_member ON plan_passengers;
DROP FUNCTION IF EXISTS plan_passenger_ensure_member();

-- Restore positions to its pre-0010 shape: drop the plan_part_id column,
-- re-add the flights FK, and rename the index back.
ALTER TABLE positions DROP COLUMN IF EXISTS plan_part_id;
ALTER TABLE positions
    ADD CONSTRAINT positions_flight_id_fkey
    FOREIGN KEY (flight_id) REFERENCES flights(id) ON DELETE CASCADE;
ALTER INDEX positions_part_ts_idx RENAME TO positions_flight_ts_idx;

DROP TABLE IF EXISTS calendar_tokens;
DROP TABLE IF EXISTS plan_alert_optin;
DROP TABLE IF EXISTS alert_prefs;
DROP TABLE IF EXISTS plan_visibility_members;
DROP TABLE IF EXISTS plan_visibility;
DROP TABLE IF EXISTS plan_passengers;
DROP TABLE IF EXISTS excursion_details;
DROP TABLE IF EXISTS dining_details;
DROP TABLE IF EXISTS ground_details;
DROP TABLE IF EXISTS train_details;
DROP TABLE IF EXISTS hotel_details;
DROP TABLE IF EXISTS flight_details;
DROP TABLE IF EXISTS plan_parts;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS trip_tags;
DROP TABLE IF EXISTS trip_members;
DROP TABLE IF EXISTS trips;
