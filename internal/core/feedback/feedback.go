// Package feedback is the user-submitted feedback / enhancement / bug
// surface. Tenant members submit; platform_admin triages across every
// tenant. Comments hang off each item; votes are deduped per user.
//
// Why this lives in core/ instead of being a thin handler:
//   - Notify hook on Create fires `FeedbackSubmitted` on the event bus
//     so platform_admin gets a real-time Slack/email ping; the
//     dispatcher pipeline already knows how to route it.
//   - Vote/Unvote needs atomic count update + dedupe; that's a service
//     responsibility, not a handler one.
//   - Tests want to swap Repo without dragging the HTTP layer in.
package feedback

import (
	"context"
	"errors"
	"time"
)

// Type names the kind of feedback. The wizard renders an icon per type
// (bug = bug icon, etc.); the triage queue colour-codes rows by type.
type Type string

const (
	TypeBug         Type = "bug"
	TypeEnhancement Type = "enhancement"
	TypeQuestion    Type = "question"
	TypePraise      Type = "praise"
)

// AllTypes is the closed set the API + wizard enumerate.
var AllTypes = []Type{TypeBug, TypeEnhancement, TypeQuestion, TypePraise}

// Priority is set at submit time by the user; platform_admin can override
// during triage. Defaults to PriorityNormal.
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityNormal   Priority = "normal"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

var AllPriorities = []Priority{PriorityLow, PriorityNormal, PriorityHigh, PriorityCritical}

// Status walks the triage pipeline. The state machine is permissive
// (any → any) on purpose; backflow from in_progress to triaged after a
// surprise repro is a legitimate move.
type Status string

const (
	StatusNew        Status = "new"
	StatusTriaged    Status = "triaged"
	StatusInProgress Status = "in_progress"
	StatusResolved   Status = "resolved"
	StatusWontFix    Status = "wont_fix"
)

var AllStatuses = []Status{
	StatusNew, StatusTriaged, StatusInProgress, StatusResolved, StatusWontFix,
}

// Item is one feedback submission. PageURL + UserAgent are captured by
// the frontend at submit time so bug repros include the browser context
// without the user having to copy it.
type Item struct {
	ID             string
	TenantID       string
	SubmitterID    string
	SubmitterEmail string
	Type           Type
	Title          string
	Body           string
	PageURL        string
	UserAgent      string
	Priority       Priority
	Status         Status
	AssigneeID     string
	VoteCount      int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Comment is one entry in the triage thread. AuthorRole distinguishes
// platform-side notes ("internal") from submitter replies ("submitter")
// so the UI can render them with different chrome.
type Comment struct {
	ID         string
	ItemID     string
	AuthorID   string
	AuthorRole string
	Body       string
	CreatedAt  time.Time
}

// ListOptions filters the triage queue. All zero-value fields mean
// "any." Limit defaults to 50 when zero (capped at 200).
type ListOptions struct {
	TenantID string // empty = cross-tenant (platform_admin)
	Status   Status
	Type     Type
	Priority Priority
	Assignee string
	Limit    int
}

// ErrNotFound is returned when an id doesn't exist (or belongs to a
// different tenant for a non-platform query).
var ErrNotFound = errors.New("feedback: not found")

// Repo is the persistence interface. Memory + Postgres impls below /
// in storage/postgres.
type Repo interface {
	Create(ctx context.Context, it *Item) (*Item, error)
	Get(ctx context.Context, id string) (*Item, error)
	List(ctx context.Context, opts ListOptions) ([]*Item, error)
	Update(ctx context.Context, it *Item) (*Item, error)
	Delete(ctx context.Context, id string) error
	// Vote returns (newCount, alreadyVoted). When alreadyVoted is true
	// the count is unchanged — the table dedupes per voter.
	Vote(ctx context.Context, itemID, voterID string) (int, bool, error)
	Unvote(ctx context.Context, itemID, voterID string) (int, error)
	VotedBy(ctx context.Context, voterID string, itemIDs []string) (map[string]bool, error)
	AddComment(ctx context.Context, c *Comment) (*Comment, error)
	ListComments(ctx context.Context, itemID string) ([]*Comment, error)
}
