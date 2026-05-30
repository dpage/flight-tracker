-- Reverse 0012: drop the alert dedupe signature column.
ALTER TABLE flight_details DROP COLUMN IF EXISTS last_alert_sig;
