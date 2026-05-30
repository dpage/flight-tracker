-- Reverse 0014: drop the gate/terminal columns.
ALTER TABLE flight_details DROP COLUMN IF EXISTS origin_gate;
ALTER TABLE flight_details DROP COLUMN IF EXISTS dest_gate;
ALTER TABLE flight_details DROP COLUMN IF EXISTS origin_terminal;
ALTER TABLE flight_details DROP COLUMN IF EXISTS dest_terminal;
