-- Track whether a position came from a real ADS-B / airline source or was
-- extrapolated by the dead-reckoner when ADS-B coverage dropped out (e.g.
-- oceanic gaps). The frontend renders estimated positions differently so
-- users know not to take a four-hour-old extrapolation as live truth.
ALTER TABLE positions
    ADD COLUMN is_estimated BOOLEAN NOT NULL DEFAULT FALSE;

-- The 24-bit ICAO aircraft address (six hex chars, e.g. 'A1B2C3'). Required
-- by trackers that key on the airframe rather than the callsign — OpenSky's
-- /api/states/all takes an icao24 filter. Nullable so the existing manual /
-- stub flow keeps working without it.
ALTER TABLE flights
    ADD COLUMN icao24 TEXT;
