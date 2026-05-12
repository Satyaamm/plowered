"use client";

import { useMemo, useState, useEffect } from "react";
import {
  Dropdown,
  Field,
  InfoLabel,
  Input,
  Option,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { TimePicker } from "@fluentui/react-timepicker-compat";

/**
 * Friendly cron scheduler.
 *
 * Most users don't want to remember the five-field cron grammar — they
 * want to say "every day at 03:00 IST". The visual mode picks one of
 * five common patterns and assembles a valid cron expression from the
 * sub-controls. The advanced mode flips back to a raw cron input for
 * the edge cases (every 15 min weekdays, last Friday of the month).
 *
 * The cron expression is always emitted in the chosen timezone — the
 * IANA tz string is surfaced alongside so the backend / scheduler can
 * apply the same offset on its end.
 */

type Frequency = "minute" | "hour" | "day" | "week" | "month" | "custom";

const FREQ_OPTIONS: { value: Frequency; label: string; example: string }[] = [
  { value: "minute", label: "Every N minutes", example: "*/15 * * * *" },
  { value: "hour",   label: "Hourly",          example: "0 * * * *" },
  { value: "day",    label: "Daily",           example: "0 3 * * *" },
  { value: "week",   label: "Weekly",          example: "0 3 * * 1" },
  { value: "month",  label: "Monthly",         example: "0 3 1 * *" },
  { value: "custom", label: "Custom (advanced)", example: "raw cron" },
];

const WEEKDAYS = [
  { value: "1", label: "Monday" },
  { value: "2", label: "Tuesday" },
  { value: "3", label: "Wednesday" },
  { value: "4", label: "Thursday" },
  { value: "5", label: "Friday" },
  { value: "6", label: "Saturday" },
  { value: "0", label: "Sunday" },
];

// IANA timezones the picker offers. A short curated list covers 95% of
// real customers; the textual Input still allows anything if needed.
const TIMEZONES = [
  "UTC",
  "Asia/Kolkata",
  "America/Los_Angeles",
  "America/New_York",
  "America/Chicago",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Paris",
  "Asia/Singapore",
  "Asia/Tokyo",
  "Asia/Shanghai",
  "Australia/Sydney",
];

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "12px" },
  modeRow: { display: "flex", alignItems: "center", gap: "12px" },
  modeHint: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  grid: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    gap: "12px",
  },
  cronPreview: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    color: tokens.colorBrandForeground1,
    fontSize: "12px",
    padding: "6px 10px",
    backgroundColor: "#F3EEFE",
    borderRadius: "4px",
    display: "inline-block",
  },
});

export interface CronValue {
  cron: string;
  timezone: string;
}

export function CronPicker({
  value,
  onChange,
  required = false,
  error,
}: {
  value: CronValue;
  onChange: (v: CronValue) => void;
  required?: boolean;
  error?: string;
}) {
  const styles = useStyles();

  // Parse the current cron back into a frequency + sub-fields when the
  // component first mounts so editing an existing pipeline lands the
  // user in the right preset. Anything we can't parse drops to custom.
  const initial = useMemo(() => parseCron(value.cron), [value.cron]);
  const [freq, setFreq] = useState<Frequency>(initial.freq);
  const [minutes, setMinutes] = useState<string>(initial.minutes);
  const [hour, setHour] = useState<string>(initial.hour);
  const [minute, setMinute] = useState<string>(initial.minute);
  const [weekday, setWeekday] = useState<string>(initial.weekday);
  const [domDay, setDomDay] = useState<string>(initial.dom);
  const [custom, setCustom] = useState<string>(value.cron);

  // Recompute the cron string whenever any sub-field changes. We only
  // bubble it up when it differs to avoid loops.
  useEffect(() => {
    const cron = buildCron({ freq, minutes, hour, minute, weekday, domDay, custom });
    if (cron !== value.cron) {
      onChange({ ...value, cron });
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [freq, minutes, hour, minute, weekday, domDay, custom]);

  return (
    <div className={styles.root}>
      <Field
        required={required}
        label={
          <InfoLabel
            info="Five-field cron expression in the chosen timezone. Use the Frequency dropdown for common schedules; switch to Custom for anything the presets don't cover."
          >
            Schedule
          </InfoLabel>
        }
        validationState={error ? "error" : "none"}
        validationMessage={error}
      >
        <Dropdown
          value={FREQ_OPTIONS.find((o) => o.value === freq)?.label ?? "Daily"}
          selectedOptions={[freq]}
          onOptionSelect={(_, d) => setFreq((d.optionValue ?? "day") as Frequency)}
        >
          {FREQ_OPTIONS.map((o) => (
            <Option key={o.value} value={o.value}>
              {o.label}
            </Option>
          ))}
        </Dropdown>
      </Field>

      {freq === "minute" && (
        <Field
          label={
            <InfoLabel info="Plowered fires the pipeline every N minutes. Minimum 1 minute. For sub-minute scheduling, use an external trigger and POST /v1/pipelines/{id}/trigger.">
              Every N minutes
            </InfoLabel>
          }
        >
          <Input
            type="number"
            min={1}
            max={59}
            value={minutes}
            onChange={(_, d) => setMinutes(d.value || "15")}
            style={{ width: 120 }}
          />
        </Field>
      )}

      {(freq === "hour") && (
        <Field
          label={
            <InfoLabel info="Pipeline fires once per hour, at this minute offset within the hour (0-59).">
              Minute offset (within each hour)
            </InfoLabel>
          }
        >
          <Input
            type="number"
            min={0}
            max={59}
            value={minute}
            onChange={(_, d) => setMinute(d.value || "0")}
            style={{ width: 120 }}
          />
        </Field>
      )}

      {(freq === "day" || freq === "week" || freq === "month") && (
        <Field
          label={
            <InfoLabel info="Pick the time-of-day in the selected timezone. The clock-picker drives the hour + minute slots of the underlying cron expression directly — what you pick is exactly when the pipeline fires.">
              Time of day
            </InfoLabel>
          }
        >
          <TimePicker
            hourCycle="h23"
            increment={15}
            startHour={0}
            endHour={24}
            selectedTime={hourMinuteToDate(hour, minute)}
            onTimeChange={(_, data) => {
              const d = data.selectedTime;
              if (!d) return;
              setHour(String(d.getHours()));
              setMinute(String(d.getMinutes()));
            }}
            freeform
            onInput={(e) => {
              const v = (e.target as HTMLInputElement).value;
              const match = /^(\d{1,2}):(\d{2})$/.exec(v.trim());
              if (match) {
                const h = Math.max(0, Math.min(23, Number(match[1])));
                const m = Math.max(0, Math.min(59, Number(match[2])));
                setHour(String(h));
                setMinute(String(m));
              }
            }}
            style={{ width: 180 }}
            formatDateToTimeString={formatHHMM}
          />
        </Field>
      )}

      {freq === "week" && (
        <Field
          label={
            <InfoLabel info="Day of the week the pipeline runs. For multi-day schedules (Mon-Fri, weekends), switch to Custom and use a comma list.">
              Day of week
            </InfoLabel>
          }
        >
          <Dropdown
            value={WEEKDAYS.find((d) => d.value === weekday)?.label ?? "Monday"}
            selectedOptions={[weekday]}
            onOptionSelect={(_, d) => setWeekday(d.optionValue ?? "1")}
          >
            {WEEKDAYS.map((w) => (
              <Option key={w.value} value={w.value}>
                {w.label}
              </Option>
            ))}
          </Dropdown>
        </Field>
      )}

      {freq === "month" && (
        <Field
          label={
            <InfoLabel info="Day of the month (1-31). If the month has fewer days the run is skipped — use 'L' in Custom mode for last-day-of-month.">
              Day of month
            </InfoLabel>
          }
        >
          <Input
            type="number"
            min={1}
            max={31}
            value={domDay}
            onChange={(_, d) => setDomDay(d.value || "1")}
            style={{ width: 120 }}
          />
        </Field>
      )}

      {freq === "custom" && (
        <Field
          required
          label={
            <InfoLabel info="Five-field cron: minute hour day-of-month month day-of-week. Supports *, lists (1,3,5), ranges (1-5), and steps (*/15). Examples: '0 9 * * 1-5' = weekdays at 9am; '*/30 * * * *' = every 30 minutes.">
              Cron expression
            </InfoLabel>
          }
        >
          <Input
            value={custom}
            onChange={(_, d) => setCustom(d.value)}
            placeholder="0 3 * * *"
            style={{ fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" }}
          />
        </Field>
      )}

      <Field
        label={
          <InfoLabel info="The cron fields above are interpreted in this timezone. Plowered honors DST — 03:00 local stays 03:00 local. Defaults to UTC; tenants in a single region should set this to their local zone.">
            Timezone
          </InfoLabel>
        }
      >
        <Dropdown
          value={value.timezone}
          selectedOptions={[value.timezone]}
          onOptionSelect={(_, d) =>
            onChange({ ...value, timezone: d.optionValue ?? "UTC" })
          }
        >
          {TIMEZONES.map((tz) => (
            <Option key={tz} value={tz}>
              {tz}
            </Option>
          ))}
        </Dropdown>
      </Field>

      <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
        <span className={styles.modeHint}>Resolves to:</span>
        <code className={styles.cronPreview}>
          {value.cron || "—"} ({value.timezone})
        </code>
      </div>
    </div>
  );
}

// Convert the hour/minute strings the picker holds into a Date the
// Fluent TimePicker can render. Date is just a carrier here — only the
// HH:MM portion is consumed back out by the cron builder.
function hourMinuteToDate(h: string, m: string): Date {
  const d = new Date();
  d.setHours(Number(h) || 0, Number(m) || 0, 0, 0);
  return d;
}

// 24-hour HH:MM formatter so the picker's text representation matches
// the cron-expression preview byte-for-byte.
function formatHHMM(date: Date): string {
  return `${String(date.getHours()).padStart(2, "0")}:${String(
    date.getMinutes(),
  ).padStart(2, "0")}`;
}

/**
 * Compose a cron expression from the picker's sub-controls. We always
 * emit a five-field expression compatible with the robfig/cron parser
 * the scheduler uses.
 */
function buildCron(args: {
  freq: Frequency;
  minutes: string;
  hour: string;
  minute: string;
  weekday: string;
  domDay: string;
  custom: string;
}): string {
  const n = (s: string, fallback = "0") => (s.trim() === "" ? fallback : s.trim());
  switch (args.freq) {
    case "minute":
      return `*/${n(args.minutes, "15")} * * * *`;
    case "hour":
      return `${n(args.minute)} * * * *`;
    case "day":
      return `${n(args.minute)} ${n(args.hour)} * * *`;
    case "week":
      return `${n(args.minute)} ${n(args.hour)} * * ${n(args.weekday, "1")}`;
    case "month":
      return `${n(args.minute)} ${n(args.hour)} ${n(args.domDay, "1")} * *`;
    case "custom":
      return args.custom.trim();
  }
}

/**
 * Best-effort reverse of buildCron — enough to land the user back in
 * the right preset on edit. Anything ambiguous falls to custom mode.
 */
function parseCron(cron: string): {
  freq: Frequency;
  minutes: string;
  hour: string;
  minute: string;
  weekday: string;
  dom: string;
} {
  const parts = (cron || "").trim().split(/\s+/);
  const fallback = {
    freq: "day" as Frequency,
    minutes: "15",
    hour: "3",
    minute: "0",
    weekday: "1",
    dom: "1",
  };
  if (parts.length !== 5) return cron ? { ...fallback, freq: "custom" } : fallback;
  const [m, h, dom, mon, dow] = parts;
  // */N * * * *
  if (m.startsWith("*/") && h === "*" && dom === "*" && mon === "*" && dow === "*") {
    return { ...fallback, freq: "minute", minutes: m.slice(2) };
  }
  // M * * * *
  if (h === "*" && dom === "*" && mon === "*" && dow === "*" && /^\d+$/.test(m)) {
    return { ...fallback, freq: "hour", minute: m };
  }
  // M H * * *
  if (dom === "*" && mon === "*" && dow === "*" && /^\d+$/.test(m) && /^\d+$/.test(h)) {
    return { ...fallback, freq: "day", minute: m, hour: h };
  }
  // M H * * DOW
  if (dom === "*" && mon === "*" && /^\d+$/.test(m) && /^\d+$/.test(h) && /^\d$/.test(dow)) {
    return { ...fallback, freq: "week", minute: m, hour: h, weekday: dow };
  }
  // M H DOM * *
  if (mon === "*" && dow === "*" && /^\d+$/.test(m) && /^\d+$/.test(h) && /^\d+$/.test(dom)) {
    return { ...fallback, freq: "month", minute: m, hour: h, dom };
  }
  return { ...fallback, freq: "custom" };
}
