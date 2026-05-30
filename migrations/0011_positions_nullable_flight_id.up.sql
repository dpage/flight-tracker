-- Wave 1C: positions re-key (spec §3.1, §7).
--
-- 0010 added positions.plan_part_id and dropped the flights FK, but left the
-- legacy positions.flight_id column NOT NULL. The poller now keys position
-- inserts on plan_part_id alone (flight_id is dead weight kept only so a
-- pre-Wave-3 rollback can still restore the old shape), so the NOT NULL
-- constraint has to be relaxed — otherwise a part-keyed insert that supplies
-- no flight_id fails. Wave 3 drops the column entirely with the legacy tables.
ALTER TABLE positions ALTER COLUMN flight_id DROP NOT NULL;
