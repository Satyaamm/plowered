"use client";

import { useMemo, useState } from "react";
import {
  Badge,
  Body1,
  Button,
  Caption1,
  Card,
  CardHeader,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Dropdown,
  Field,
  Input,
  Option,
  Spinner,
  Subtitle1,
  Subtitle2,
  Text,
  Textarea,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Filter20Regular,
  Person20Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { PageIntro } from "@/components/page-intro";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import {
  FeedbackItem,
  FeedbackPriority,
  FeedbackStatus,
  FeedbackType,
  useCommentFeedback,
  useDeleteFeedback,
  useFeedbackComments,
  useFeedbackList,
  useRole,
  useTriageFeedback,
} from "@/lib/hooks";

const TYPES: FeedbackType[] = ["bug", "enhancement", "question", "praise"];
const STATUSES: FeedbackStatus[] = [
  "new",
  "triaged",
  "in_progress",
  "resolved",
  "wont_fix",
];
const PRIORITIES: FeedbackPriority[] = ["low", "normal", "high", "critical"];

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  toolbar: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr 1fr 1fr",
    gap: "12px",
    padding: "12px 14px",
    backgroundColor: tokens.colorNeutralBackground1,
    borderRadius: "6px",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
  },
  card: { padding: "12px 14px", display: "flex", flexDirection: "column", gap: "8px" },
  row: { display: "flex", alignItems: "center", gap: "12px", flexWrap: "wrap" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  body: { fontSize: "13px", whiteSpace: "pre-wrap", lineHeight: 1.5 },
  pageURL: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "11px",
    color: tokens.colorNeutralForeground3,
    wordBreak: "break-all",
  },
  triageBlock: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr 1fr",
    gap: "12px",
  },
  comments: {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    marginTop: "8px",
  },
  comment: {
    padding: "8px 10px",
    backgroundColor: tokens.colorNeutralBackground2,
    borderRadius: "4px",
    fontSize: "13px",
  },
  commentAuthor: {
    fontSize: "11px",
    color: tokens.colorNeutralForeground3,
    marginBottom: "2px",
  },
});

function typeColor(t: FeedbackType): "danger" | "brand" | "warning" | "success" {
  if (t === "bug") return "danger";
  if (t === "enhancement") return "brand";
  if (t === "question") return "warning";
  return "success";
}

function priorityColor(p: FeedbackPriority): "danger" | "warning" | "subtle" {
  if (p === "critical" || p === "high") return "danger";
  if (p === "normal") return "warning";
  return "subtle";
}

function statusColor(s: FeedbackStatus): "informative" | "warning" | "success" | "subtle" {
  if (s === "new") return "informative";
  if (s === "in_progress" || s === "triaged") return "warning";
  if (s === "resolved") return "success";
  return "subtle";
}

export default function AdminFeedbackPage() {
  const styles = useStyles();
  const { can } = useRole();
  const isPlatformAdmin = can("platform");
  const [status, setStatus] = useState<FeedbackStatus | "">("");
  const [type, setType] = useState<FeedbackType | "">("");
  const [priority, setPriority] = useState<FeedbackPriority | "">("");
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const list = useFeedbackList({
    status,
    type,
    priority,
    cross_tenant: isPlatformAdmin,
  });

  return (
    <div className={styles.root}>
      <PageHeader
        title="Feedback triage"
        crumbs={[{ label: "Admin" }, { label: "Feedback" }]}
        actions={
          <PageIntro
            title="Triage user feedback"
            body="Every bug report, enhancement request, question, and praise note submitted via the topbar Feedback button lands here. Platform admins see the queue across all tenants; tenant admins see only their own. Status drives the pipeline (new → triaged → in_progress → resolved | wont_fix); vote_count surfaces the most-wanted items."
            bullets={[
              "Filter by status / type / priority. Vote count sorts which enhancements to ship next.",
              "Comments form the triage thread — platform_admin notes vs. submitter replies render with different chrome.",
              "PageURL + browser UA are auto-captured at submit time so bug repros include the context.",
            ]}
            cta="Tip: respond on a feedback item even with 'we're tracking this' — closing the loop matters for users."
          />
        }
      />

      <div className={styles.toolbar}>
        <Field label="Status">
          <Dropdown
            value={status || "All"}
            selectedOptions={[status]}
            onOptionSelect={(_, d) =>
              setStatus((d.optionValue as FeedbackStatus) || "")
            }
          >
            <Option value="">All</Option>
            {STATUSES.map((s) => (
              <Option key={s} value={s}>
                {s}
              </Option>
            ))}
          </Dropdown>
        </Field>
        <Field label="Type">
          <Dropdown
            value={type || "All"}
            selectedOptions={[type]}
            onOptionSelect={(_, d) =>
              setType((d.optionValue as FeedbackType) || "")
            }
          >
            <Option value="">All</Option>
            {TYPES.map((t) => (
              <Option key={t} value={t}>
                {t}
              </Option>
            ))}
          </Dropdown>
        </Field>
        <Field label="Priority">
          <Dropdown
            value={priority || "All"}
            selectedOptions={[priority]}
            onOptionSelect={(_, d) =>
              setPriority((d.optionValue as FeedbackPriority) || "")
            }
          >
            <Option value="">All</Option>
            {PRIORITIES.map((p) => (
              <Option key={p} value={p}>
                {p}
              </Option>
            ))}
          </Dropdown>
        </Field>
        <div style={{ display: "flex", alignItems: "flex-end" }}>
          <Caption1 className={styles.meta}>
            <Filter20Regular style={{ verticalAlign: "middle" }} />{" "}
            {list.data?.length ?? 0} item{list.data?.length === 1 ? "" : "s"}
            {isPlatformAdmin ? " (cross-tenant)" : ""}
          </Caption1>
        </div>
      </div>

      {list.isLoading && <LoadingState />}
      {list.error && <ErrorBanner error={list.error as Error} />}
      {list.data && list.data.length === 0 && (
        <EmptyState
          title="No feedback yet"
          body="When users submit feedback from the topbar drawer, it shows up here."
        />
      )}
      {list.data?.map((it) => (
        <ItemCard
          key={it.id}
          item={it}
          expanded={expandedId === it.id}
          onToggle={() => setExpandedId(expandedId === it.id ? null : it.id)}
          showTriage={isPlatformAdmin}
        />
      ))}
    </div>
  );
}

function ItemCard({
  item,
  expanded,
  onToggle,
  showTriage,
}: {
  item: FeedbackItem;
  expanded: boolean;
  onToggle: () => void;
  showTriage: boolean;
}) {
  const styles = useStyles();
  return (
    <Card className={styles.card}>
      <div className={styles.row}>
        <Badge appearance="filled" color={typeColor(item.type)}>
          {item.type}
        </Badge>
        <Badge appearance="tint" color={statusColor(item.status)}>
          {item.status}
        </Badge>
        <Badge appearance="outline" color={priorityColor(item.priority)}>
          {item.priority}
        </Badge>
        <Subtitle1 style={{ flex: 1 }}>{item.title}</Subtitle1>
        <Caption1 className={styles.meta}>
          {item.vote_count} vote{item.vote_count === 1 ? "" : "s"}
        </Caption1>
        <Button size="small" appearance="subtle" onClick={onToggle}>
          {expanded ? "Hide" : "Open"}
        </Button>
      </div>
      <Caption1 className={styles.meta}>
        {item.submitter_email || item.submitter_id} ·{" "}
        {new Date(item.created_at).toLocaleString()}
        {item.assignee_id ? ` · assigned to ${item.assignee_id}` : ""}
        {showTriage ? ` · tenant ${item.tenant_id}` : ""}
      </Caption1>
      {expanded && <ExpandedItem item={item} showTriage={showTriage} />}
    </Card>
  );
}

function ExpandedItem({
  item,
  showTriage,
}: {
  item: FeedbackItem;
  showTriage: boolean;
}) {
  const styles = useStyles();
  const triage = useTriageFeedback(item.id);
  const del = useDeleteFeedback();
  const [confirmDelete, setConfirmDelete] = useState(false);
  return (
    <>
      {item.body && <Body1 className={styles.body}>{item.body}</Body1>}
      {item.page_url && (
        <Caption1 className={styles.pageURL}>page: {item.page_url}</Caption1>
      )}
      {item.user_agent && (
        <Caption1 className={styles.pageURL}>browser: {item.user_agent}</Caption1>
      )}

      {showTriage && (
        <div className={styles.triageBlock}>
          <Field label="Status">
            <Dropdown
              value={item.status}
              selectedOptions={[item.status]}
              onOptionSelect={(_, d) =>
                triage.mutate({ status: d.optionValue as FeedbackStatus })
              }
            >
              {STATUSES.map((s) => (
                <Option key={s} value={s}>
                  {s}
                </Option>
              ))}
            </Dropdown>
          </Field>
          <Field label="Priority">
            <Dropdown
              value={item.priority}
              selectedOptions={[item.priority]}
              onOptionSelect={(_, d) =>
                triage.mutate({ priority: d.optionValue as FeedbackPriority })
              }
            >
              {PRIORITIES.map((p) => (
                <Option key={p} value={p}>
                  {p}
                </Option>
              ))}
            </Dropdown>
          </Field>
          <Field label="Assignee">
            <AssigneeInput
              current={item.assignee_id ?? ""}
              onCommit={(v) => triage.mutate({ assignee_id: v })}
            />
          </Field>
        </div>
      )}

      <CommentThread itemID={item.id} />

      {showTriage && (
        <div style={{ display: "flex", justifyContent: "flex-end" }}>
          <Button
            appearance="subtle"
            onClick={() => setConfirmDelete(true)}
          >
            Delete
          </Button>
          <Dialog open={confirmDelete} onOpenChange={(_, d) => !d.open && setConfirmDelete(false)}>
            <DialogSurface>
              <DialogBody>
                <DialogTitle>Delete this feedback item?</DialogTitle>
                <DialogContent>
                  <Text>
                    The item, its votes, and its comment thread will be
                    removed permanently. The submitter is not notified.
                  </Text>
                </DialogContent>
                <DialogActions>
                  <Button onClick={() => setConfirmDelete(false)}>Cancel</Button>
                  <Button
                    appearance="primary"
                    onClick={async () => {
                      await del.mutateAsync(item.id);
                      setConfirmDelete(false);
                    }}
                  >
                    Delete
                  </Button>
                </DialogActions>
              </DialogBody>
            </DialogSurface>
          </Dialog>
        </div>
      )}
    </>
  );
}

function AssigneeInput({
  current,
  onCommit,
}: {
  current: string;
  onCommit: (v: string) => void;
}) {
  const [value, setValue] = useState(current);
  return (
    <Input
      value={value}
      contentBefore={<Person20Regular />}
      onChange={(_, d) => setValue(d.value)}
      onBlur={() => {
        if (value !== current) onCommit(value);
      }}
      placeholder="user id"
    />
  );
}

function CommentThread({ itemID }: { itemID: string }) {
  const styles = useStyles();
  const comments = useFeedbackComments(itemID);
  const addComment = useCommentFeedback(itemID);
  const [draft, setDraft] = useState("");

  const ordered = useMemo(
    () =>
      (comments.data ?? [])
        .slice()
        .sort((a, b) => a.CreatedAt.localeCompare(b.CreatedAt)),
    [comments.data],
  );

  const send = async () => {
    if (!draft.trim()) return;
    await addComment.mutateAsync(draft.trim());
    setDraft("");
  };

  return (
    <Card style={{ padding: "10px 12px" }}>
      <CardHeader header={<Subtitle2>Comments</Subtitle2>} />
      <div className={styles.comments}>
        {ordered.length === 0 && (
          <Caption1 className={styles.meta}>No comments yet.</Caption1>
        )}
        {ordered.map((c) => (
          <div key={c.ID} className={styles.comment}>
            <div className={styles.commentAuthor}>
              {c.AuthorRole === "platform_admin" ? "🛠 platform admin" : "submitter"} ·{" "}
              {new Date(c.CreatedAt).toLocaleString()}
            </div>
            {c.Body}
          </div>
        ))}
      </div>
      <div style={{ display: "flex", gap: 8, alignItems: "flex-start", marginTop: 8 }}>
        <Textarea
          rows={2}
          value={draft}
          onChange={(_, d) => setDraft(d.value.slice(0, 5000))}
          placeholder="Reply…"
          style={{ flex: 1 }}
        />
        <Button
          appearance="primary"
          onClick={send}
          disabled={!draft.trim() || addComment.isPending}
        >
          {addComment.isPending ? <Spinner size="extra-tiny" /> : "Post"}
        </Button>
      </div>
    </Card>
  );
}
