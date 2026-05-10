-- 0008_user_profile: capture first/last name + phone for the signup form.
--
-- We keep the existing `full_name` column populated as `first_name || ' ' || last_name`
-- so older code paths that read full_name continue to work without a join.
-- phone_country is the dial-code (e.g. "+1", "+91") and phone is the
-- subscriber number (digits only); keeping them split makes E.164 reformat
-- and country-by-country listing trivial.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS first_name    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_name     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS phone         TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS phone_country TEXT NOT NULL DEFAULT '';
