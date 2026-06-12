"use client";

import { useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import {
  Button,
  Caption1,
  Drawer,
  DrawerBody,
  DrawerHeader,
  DrawerHeaderTitle,
  Dropdown,
  Field,
  Input,
  Option,
  Spinner,
  Textarea,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Dismiss24Regular } from "@fluentui/react-icons";
import {
  FeedbackPriority,
  FeedbackType,
  useSubmitFeedback,
} from "@/lib/hooks";

// FeedbackDrawer is the platform-wide "send us feedback" sheet. Lives
// in the topbar so it's reachable from every page; auto-captures the
// current pathname + browser UA so a bug report includes repro context
// without the user having to copy anything.

const TITLE_MAX = 200;
const BODY_MAX = 5000;

const TYPES: { value: FeedbackType; label: string; description: string }[] = [
  { value: "bug",         label: "Bug",         description: "Something is broken or behaves unexpectedly" },
  { value: "enhancement", label: "Enhancement", description: "An idea to make a feature more useful" },
  { value: "question",    label: "Question",    description: "Not sure how something works" },
  { value: "praise",      label: "Praise",      description: "Something you love (we read these too)" },
];

const PRIORITIES: FeedbackPriority[] = ["low", "normal", "high", "critical"];

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "14px", padding: "16px 0" },
  counterRow: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
  },
  counter: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  counterWarn: { color: tokens.colorPaletteRedForeground1, fontSize: "12px" },
  hint: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  captureBox: {
    backgroundColor: tokens.colorNeutralBackground2,
    border: `1px dashed ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "10px 12px",
    fontSize: "12px",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    color: tokens.colorNeutralForeground3,
    whiteSpace: "pre-wrap",
    wordBreak: "break-all",
  },
  footer: {
    display: "flex",
    gap: "8px",
    justifyContent: "flex-end",
    paddingTop: "12px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    marginTop: "auto",
  },
});

export function FeedbackDrawer({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const styles = useStyles();
  const pathname = usePathname();
  const submit = useSubmitFeedback();
  const [type, setType] = useState<FeedbackType>("bug");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [priority, setPriority] = useState<FeedbackPriority>("normal");

  // Reset form when the drawer reopens so a previous draft doesn't
  // ghost in. (Keep the type if the user opened it deliberately for a
  // bug — but they can switch with one click anyway.)
  useEffect(() => {
    if (open) {
      setTitle("");
      setBody("");
      setPriority("normal");
    }
  }, [open]);

  const titleOver = title.length > TITLE_MAX;
  const bodyOver = body.length > BODY_MAX;
  const canSubmit = title.trim() !== "" && !titleOver && !bodyOver;

  const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
  const url =
    typeof window !== "undefined" ? window.location.href : pathname ?? "";

  const onSubmit = async () => {
    if (!canSubmit) return;
    try {
      await submit.mutateAsync({
        type,
        title: title.trim(),
        body: body.trim(),
        priority,
      });
      onClose();
    } catch {
      // toast surfaces the error; keep the form filled so the user can retry.
    }
  };

  return (
    <Drawer
      type="overlay"
      separator
      open={open}
      onOpenChange={(_, d) => !d.open && onClose()}
      position="end"
      size="medium"
    >
      <DrawerHeader>
        <DrawerHeaderTitle
          action={
            <Button
              appearance="subtle"
              icon={<Dismiss24Regular />}
              onClick={onClose}
              aria-label="Close feedback drawer"
            />
          }
        >
          Send feedback
        </DrawerHeaderTitle>
      </DrawerHeader>
      <DrawerBody>
        <div className={styles.body}>
          <Caption1 className={styles.hint}>
            Every submission lands in our triage queue and we read all of them.
            Bugs are prioritised by reproducibility; enhancements by vote count.
          </Caption1>

          <Field label="What kind of feedback?" required>
            <Dropdown
              value={TYPES.find((t) => t.value === type)?.label ?? ""}
              selectedOptions={[type]}
              onOptionSelect={(_, d) => setType(d.optionValue as FeedbackType)}
            >
              {TYPES.map((t) => (
                <Option key={t.value} value={t.value} text={t.label}>
                  <span>
                    <strong>{t.label}</strong>{" "}
                    <Caption1 className={styles.hint}>{t.description}</Caption1>
                  </span>
                </Option>
              ))}
            </Dropdown>
          </Field>

          <Field
            label="Title"
            required
            validationState={titleOver ? "error" : "none"}
            validationMessage={
              titleOver ? `Trim to ${TITLE_MAX} characters or fewer.` : undefined
            }
          >
            <Input
              value={title}
              onChange={(_, d) => setTitle(d.value.slice(0, TITLE_MAX + 1))}
              placeholder={
                type === "bug"
                  ? "Short summary — e.g. 'Asset detail page crashes on click'"
                  : "One-line summary of your idea"
              }
              maxLength={TITLE_MAX + 1}
            />
          </Field>

          <Field
            label="Details"
            validationState={bodyOver ? "error" : "none"}
            validationMessage={
              bodyOver ? `Trim to ${BODY_MAX} characters or fewer.` : undefined
            }
          >
            <Textarea
              rows={8}
              value={body}
              onChange={(_, d) => setBody(d.value.slice(0, BODY_MAX + 1))}
              placeholder={
                type === "bug"
                  ? "What did you do? What did you expect? What actually happened?"
                  : "Give us context — what problem would this solve, who benefits, what does success look like?"
              }
            />
          </Field>

          <div className={styles.counterRow}>
            <Field label="Priority">
              <Dropdown
                value={priority}
                selectedOptions={[priority]}
                onOptionSelect={(_, d) =>
                  setPriority(d.optionValue as FeedbackPriority)
                }
              >
                {PRIORITIES.map((p) => (
                  <Option key={p} value={p} text={p}>
                    {p}
                  </Option>
                ))}
              </Dropdown>
            </Field>
            <span className={bodyOver ? styles.counterWarn : styles.counter}>
              {body.length} / {BODY_MAX}
            </span>
          </div>

          <Field label="What we'll capture automatically">
            <div className={styles.captureBox}>
              page: {url}
              {"\n"}browser: {ua}
            </div>
          </Field>

          <div className={styles.footer}>
            <Button onClick={onClose} disabled={submit.isPending}>
              Cancel
            </Button>
            <Button
              appearance="primary"
              onClick={onSubmit}
              disabled={!canSubmit || submit.isPending}
            >
              {submit.isPending ? <Spinner size="extra-tiny" /> : "Send feedback"}
            </Button>
          </div>
        </div>
      </DrawerBody>
    </Drawer>
  );
}
