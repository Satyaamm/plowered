package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/feedback"
)

// FeedbackStore is the Postgres-backed Repo for feedback items, votes,
// and comments. Vote operations run in a transaction so the
// item.vote_count cache + the votes table stay consistent under
// concurrent voters.
type FeedbackStore struct {
	pool *pgxpool.Pool
}

func NewFeedbackStore(p *pgxpool.Pool) *FeedbackStore { return &FeedbackStore{pool: p} }

func (s *FeedbackStore) Create(ctx context.Context, it *feedback.Item) (*feedback.Item, error) {
	const q = `
		INSERT INTO feedback_items
		    (tenant_id, submitter_id, submitter_email, type, title, body,
		     page_url, user_agent, priority, status, assignee_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id::text, created_at, updated_at`
	priority := it.Priority
	if priority == "" {
		priority = feedback.PriorityNormal
	}
	status := it.Status
	if status == "" {
		status = feedback.StatusNew
	}
	if err := s.pool.QueryRow(ctx, q,
		it.TenantID, it.SubmitterID, it.SubmitterEmail,
		string(it.Type), it.Title, it.Body,
		it.PageURL, it.UserAgent,
		string(priority), string(status), it.AssigneeID,
	).Scan(&it.ID, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create feedback: %w", err)
	}
	it.Priority = priority
	it.Status = status
	return it, nil
}

func (s *FeedbackStore) Get(ctx context.Context, id string) (*feedback.Item, error) {
	const q = `
		SELECT id::text, tenant_id, submitter_id, submitter_email,
		       type, title, body, page_url, user_agent,
		       priority, status, assignee_id, vote_count,
		       created_at, updated_at
		  FROM feedback_items
		 WHERE id = $1::uuid`
	return scanFeedback(s.pool.QueryRow(ctx, q, id))
}

func (s *FeedbackStore) List(ctx context.Context, opts feedback.ListOptions) ([]*feedback.Item, error) {
	args := []any{}
	where := []string{"TRUE"}
	add := func(sql string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(sql, len(args)))
	}
	if opts.TenantID != "" {
		add("tenant_id = $%d", opts.TenantID)
	}
	if opts.Status != "" {
		add("status = $%d", string(opts.Status))
	}
	if opts.Type != "" {
		add("type = $%d", string(opts.Type))
	}
	if opts.Priority != "" {
		add("priority = $%d", string(opts.Priority))
	}
	if opts.Assignee != "" {
		add("assignee_id = $%d", opts.Assignee)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	q := `
		SELECT id::text, tenant_id, submitter_id, submitter_email,
		       type, title, body, page_url, user_agent,
		       priority, status, assignee_id, vote_count,
		       created_at, updated_at
		  FROM feedback_items
		 WHERE ` + strings.Join(where, " AND ") + `
		 ORDER BY created_at DESC
		 LIMIT ` + fmt.Sprint(limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	defer rows.Close()
	out := []*feedback.Item{}
	for rows.Next() {
		it, err := scanFeedback(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *FeedbackStore) Update(ctx context.Context, it *feedback.Item) (*feedback.Item, error) {
	const q = `
		UPDATE feedback_items
		   SET type = $2, title = $3, body = $4,
		       priority = $5, status = $6, assignee_id = $7,
		       updated_at = now()
		 WHERE id = $1::uuid
		RETURNING updated_at`
	if err := s.pool.QueryRow(ctx, q,
		it.ID, string(it.Type), it.Title, it.Body,
		string(it.Priority), string(it.Status), it.AssigneeID,
	).Scan(&it.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, feedback.ErrNotFound
		}
		return nil, fmt.Errorf("update feedback: %w", err)
	}
	return it, nil
}

func (s *FeedbackStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM feedback_items WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return feedback.ErrNotFound
	}
	return nil
}

// Vote runs INSERT ... ON CONFLICT to dedupe + a conditional count
// update in the same TX. Returns (count, alreadyVoted, err). The
// alreadyVoted flag lets the API return 200 with the unchanged count
// instead of double-counting.
func (s *FeedbackStore) Vote(ctx context.Context, itemID, voterID string) (int, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	var inserted bool
	if err := tx.QueryRow(ctx, `
		INSERT INTO feedback_votes (item_id, voter_id)
		VALUES ($1::uuid, $2)
		ON CONFLICT (item_id, voter_id) DO NOTHING
		RETURNING TRUE`,
		itemID, voterID,
	).Scan(&inserted); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	if !inserted {
		// Already voted — fetch current count and return unchanged.
		var count int
		if err := tx.QueryRow(ctx,
			`SELECT vote_count FROM feedback_items WHERE id = $1::uuid`,
			itemID,
		).Scan(&count); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, false, feedback.ErrNotFound
			}
			return 0, false, err
		}
		return count, true, tx.Commit(ctx)
	}
	var count int
	if err := tx.QueryRow(ctx, `
		UPDATE feedback_items
		   SET vote_count = vote_count + 1, updated_at = now()
		 WHERE id = $1::uuid
		RETURNING vote_count`,
		itemID,
	).Scan(&count); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, feedback.ErrNotFound
		}
		return 0, false, err
	}
	return count, false, tx.Commit(ctx)
}

func (s *FeedbackStore) Unvote(ctx context.Context, itemID, voterID string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx,
		`DELETE FROM feedback_votes WHERE item_id = $1::uuid AND voter_id = $2`,
		itemID, voterID)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		var count int
		if err := tx.QueryRow(ctx,
			`SELECT vote_count FROM feedback_items WHERE id = $1::uuid`,
			itemID,
		).Scan(&count); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, feedback.ErrNotFound
			}
			return 0, err
		}
		return count, tx.Commit(ctx)
	}
	var count int
	if err := tx.QueryRow(ctx, `
		UPDATE feedback_items
		   SET vote_count = GREATEST(0, vote_count - 1), updated_at = now()
		 WHERE id = $1::uuid
		RETURNING vote_count`,
		itemID,
	).Scan(&count); err != nil {
		return 0, err
	}
	return count, tx.Commit(ctx)
}

func (s *FeedbackStore) VotedBy(ctx context.Context, voterID string, itemIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT item_id::text FROM feedback_votes
		 WHERE voter_id = $1 AND item_id = ANY($2::uuid[])`,
		voterID, itemIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *FeedbackStore) AddComment(ctx context.Context, c *feedback.Comment) (*feedback.Comment, error) {
	const q = `
		INSERT INTO feedback_comments (item_id, author_id, author_role, body)
		VALUES ($1::uuid, $2, $3, $4)
		RETURNING id::text, created_at`
	if err := s.pool.QueryRow(ctx, q,
		c.ItemID, c.AuthorID, c.AuthorRole, c.Body,
	).Scan(&c.ID, &c.CreatedAt); err != nil {
		return nil, fmt.Errorf("add feedback comment: %w", err)
	}
	// Bump the parent's updated_at so the queue sorts by latest
	// activity, not just creation time.
	_, _ = s.pool.Exec(ctx,
		`UPDATE feedback_items SET updated_at = now() WHERE id = $1::uuid`, c.ItemID)
	return c, nil
}

func (s *FeedbackStore) ListComments(ctx context.Context, itemID string) ([]*feedback.Comment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, item_id::text, author_id, author_role, body, created_at
		  FROM feedback_comments
		 WHERE item_id = $1::uuid
		 ORDER BY created_at`,
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*feedback.Comment{}
	for rows.Next() {
		var c feedback.Comment
		if err := rows.Scan(&c.ID, &c.ItemID, &c.AuthorID, &c.AuthorRole, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

func scanFeedback(row rowScanner) (*feedback.Item, error) {
	var (
		it       feedback.Item
		typ      string
		priority string
		status   string
	)
	if err := row.Scan(
		&it.ID, &it.TenantID, &it.SubmitterID, &it.SubmitterEmail,
		&typ, &it.Title, &it.Body, &it.PageURL, &it.UserAgent,
		&priority, &status, &it.AssigneeID, &it.VoteCount,
		&it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, feedback.ErrNotFound
		}
		return nil, fmt.Errorf("scan feedback: %w", err)
	}
	it.Type = feedback.Type(typ)
	it.Priority = feedback.Priority(priority)
	it.Status = feedback.Status(status)
	return &it, nil
}

var _ feedback.Repo = (*FeedbackStore)(nil)
