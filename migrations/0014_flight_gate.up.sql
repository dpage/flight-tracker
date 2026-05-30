-- Gate-change alerts (README roadmap follow-up; AeroDataBox precursor).
--
-- The alert system (Wave 2B) already classifies status/delay changes from
-- flight_details. AeroDataBox returns the departure/arrival gate + terminal on
-- many airports, but flight_details never modelled them, so gate-change
-- detection wasn't possible. Add nullable gate/terminal columns the resolver
-- backfill/refresh writers fill: gate is UPDATABLE (it changes, and a change is
-- what we alert on); terminal is only-fill-empty like the other backfilled
-- airframe metadata. NULL means "unknown / not yet resolved".
ALTER TABLE flight_details ADD COLUMN origin_gate     TEXT;
ALTER TABLE flight_details ADD COLUMN dest_gate       TEXT;
ALTER TABLE flight_details ADD COLUMN origin_terminal TEXT;
ALTER TABLE flight_details ADD COLUMN dest_terminal   TEXT;
