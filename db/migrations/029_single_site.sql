-- Each installation serves one site, so tickets no longer record which one they came from.
ALTER TABLE tickets DROP COLUMN brand;
