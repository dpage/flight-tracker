-- Reverse 0011. Any rows inserted with a NULL flight_id (part-keyed inserts
-- from the Wave 1 poller) would block the NOT NULL restoration, so clear them
-- first — they are reconstructable from plan_part_id on a re-up if needed.
DELETE FROM positions WHERE flight_id IS NULL;
ALTER TABLE positions ALTER COLUMN flight_id SET NOT NULL;
