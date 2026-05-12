-- Track whether each user has completed the in-app product tour.
-- NULL means the tour was never run; a non-NULL timestamp gates the
-- auto-launch the frontend performs on first authenticated load.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS tour_completed_at TIMESTAMPTZ;
