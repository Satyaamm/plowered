-- 0024_feedback: user-submitted feedback / enhancement requests / bugs.
--
-- One feedback row per submission. The platform_admin role triages the
-- queue (cross-tenant); tenant members see + vote on their own tenant's
-- items. Comments are a separate table so the row body stays small + so
-- the API can paginate the thread.
--
-- Auto-captured fields (page_url, user_agent) are populated by the
-- frontend at submit time; useful for repro on bug reports. Storing them
-- on the row body (not in a JSONB attributes blob) keeps them queryable.

CREATE TABLE IF NOT EXISTS feedback_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    submitter_id    TEXT NOT NULL,
    submitter_email TEXT NOT NULL DEFAULT '',
    type            TEXT NOT NULL,          -- bug | enhancement | question | praise
    title           TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    page_url        TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    priority        TEXT NOT NULL DEFAULT 'normal',  -- low | normal | high | critical
    status          TEXT NOT NULL DEFAULT 'new',     -- new | triaged | in_progress | resolved | wont_fix
    assignee_id     TEXT NOT NULL DEFAULT '',        -- platform_admin user id
    vote_count      INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feedback_items_tenant
    ON feedback_items(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_feedback_items_status
    ON feedback_items(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_feedback_items_type
    ON feedback_items(type);

-- feedback_votes: one row per (user, item) — both the count cache on
-- the parent row AND this table get updated atomically by the vote
-- handler so we can show "voted by you" without re-querying every row.
CREATE TABLE IF NOT EXISTS feedback_votes (
    item_id     UUID NOT NULL REFERENCES feedback_items(id) ON DELETE CASCADE,
    voter_id    TEXT NOT NULL,
    voted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, voter_id)
);

-- feedback_comments: triage thread. Either side (submitter, platform
-- admin) can post; the kind column distinguishes operator notes from
-- submitter replies so the UI can render them differently.
CREATE TABLE IF NOT EXISTS feedback_comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id     UUID NOT NULL REFERENCES feedback_items(id) ON DELETE CASCADE,
    author_id   TEXT NOT NULL,
    author_role TEXT NOT NULL DEFAULT '',  -- 'platform_admin' | 'submitter' | etc.
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feedback_comments_item
    ON feedback_comments(item_id, created_at);
