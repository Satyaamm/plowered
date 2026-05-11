"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import {
  Button,
  Checkbox,
  Field,
  Input,
  Label,
  MessageBar,
  MessageBarBody,
  Spinner,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Call20Regular } from "@fluentui/react-icons";
import { AuthShell } from "@/components/auth-shell";
import { useSignup } from "@/lib/auth-client";

// Curated G20+ subset. Add rows here as customers ask; the server-side
// regex (^\+\d{1,4}$) accepts any well-formed dial code so the list is
// purely a UX affordance.
const COUNTRY_CODES: { code: string; label: string; flag: string }[] = [
  { code: "+1",   flag: "🇺🇸", label: "United States / Canada" },
  { code: "+44",  flag: "🇬🇧", label: "United Kingdom" },
  { code: "+91",  flag: "🇮🇳", label: "India" },
  { code: "+61",  flag: "🇦🇺", label: "Australia" },
  { code: "+49",  flag: "🇩🇪", label: "Germany" },
  { code: "+33",  flag: "🇫🇷", label: "France" },
  { code: "+81",  flag: "🇯🇵", label: "Japan" },
  { code: "+86",  flag: "🇨🇳", label: "China" },
  { code: "+55",  flag: "🇧🇷", label: "Brazil" },
  { code: "+34",  flag: "🇪🇸", label: "Spain" },
  { code: "+39",  flag: "🇮🇹", label: "Italy" },
  { code: "+7",   flag: "🇷🇺", label: "Russia" },
  { code: "+82",  flag: "🇰🇷", label: "South Korea" },
  { code: "+65",  flag: "🇸🇬", label: "Singapore" },
  { code: "+971", flag: "🇦🇪", label: "United Arab Emirates" },
  { code: "+27",  flag: "🇿🇦", label: "South Africa" },
  { code: "+52",  flag: "🇲🇽", label: "Mexico" },
  { code: "+62",  flag: "🇮🇩", label: "Indonesia" },
];

const useStyles = makeStyles({
  form: { display: "flex", flexDirection: "column", gap: "12px" },
  twoCol: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    gap: "12px",
  },
  // Phone block: a single bordered control with three internal slots
  // (icon | dial-code select | number input) separated by vertical
  // dividers. The native <select> renders the OS up/down chevron
  // without us having to ship our own icon — matches the Bootstrap
  // input-group convention users are familiar with.
  phoneBlock: { display: "flex", flexDirection: "column", gap: "6px" },
  phoneGroup: {
    display: "flex",
    alignItems: "stretch",
    height: "32px",
    boxShadow: `inset 0 0 0 1px ${tokens.colorNeutralStroke1}`,
    borderRadius: "4px",
    backgroundColor: tokens.colorNeutralBackground1,
    overflow: "hidden",
    transition: "box-shadow 120ms ease",
    ":focus-within": {
      boxShadow: `inset 0 0 0 2px ${tokens.colorBrandStroke1}`,
    },
  },
  phoneGroupError: {
    boxShadow: `inset 0 0 0 1px ${tokens.colorPaletteRedBorder2}`,
    ":focus-within": {
      boxShadow: `inset 0 0 0 2px ${tokens.colorPaletteRedBorder2}`,
    },
  },
  phoneIcon: {
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    width: "36px",
    color: tokens.colorNeutralForeground3,
    borderRight: `1px solid ${tokens.colorNeutralStroke2}`,
    flexShrink: 0,
  },
  phoneSelect: {
    border: "none",
    outline: "none",
    background: "transparent",
    padding: "0 10px",
    paddingRight: "26px",
    fontSize: "14px",
    color: tokens.colorNeutralForeground1,
    appearance: "none",
    cursor: "pointer",
    borderRight: `1px solid ${tokens.colorNeutralStroke2}`,
    minWidth: "92px",
    // The native browser dropdown indicator is hidden by appearance:none,
    // so paint our own up/down caret as a background SVG.
    backgroundImage:
      "url(\"data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='10' height='14' viewBox='0 0 10 14' fill='%237B6B58'><path d='M5 0L0 5h10L5 0z'/><path d='M5 14l5-5H0l5 5z'/></svg>\")",
    backgroundRepeat: "no-repeat",
    backgroundPosition: "right 8px center",
    backgroundSize: "8px 12px",
    flexShrink: 0,
  },
  phoneNumberInput: {
    border: "none",
    outline: "none",
    background: "transparent",
    padding: "0 12px",
    fontSize: "14px",
    color: tokens.colorNeutralForeground1,
    flex: 1,
    minWidth: 0,
  },
  phoneHint: {
    fontSize: "12px",
    color: tokens.colorNeutralForeground3,
    marginTop: "2px",
  },
  phoneError: {
    fontSize: "12px",
    color: tokens.colorPaletteRedForeground1,
    marginTop: "2px",
  },
  meta: {
    fontSize: "12px",
    color: tokens.colorNeutralForeground3,
    textAlign: "center",
  },
  link: { color: tokens.colorBrandForeground1, fontWeight: 600, textDecoration: "none" },
  hint: { fontSize: "11px", color: tokens.colorNeutralForeground3 },
  strengthBar: {
    height: "4px",
    borderRadius: "2px",
    backgroundColor: tokens.colorNeutralStroke2,
    overflow: "hidden",
    marginTop: "4px",
  },
  strengthFill: {
    height: "100%",
    transition: "width 120ms ease, background-color 120ms ease",
  },
});

interface FieldErrors {
  workspace?: string;
  firstName?: string;
  lastName?: string;
  email?: string;
  phone?: string;
  password?: string;
  confirm?: string;
  terms?: string;
}

// Email regex permissive on purpose; the canonical check lives server-
// side. We just want to catch obvious typos like missing "@".
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const NAME_RE = /^[\p{L}\p{M}\s'.,\-]+$/u; // letters/marks/punct only, no digits or symbols

function phoneDigits(s: string): string {
  return s.replace(/\D+/g, "");
}

function passwordScore(p: string): { score: number; label: string; color: string } {
  if (!p) return { score: 0, label: "", color: tokens.colorNeutralStroke2 };
  let classes = 0;
  if (/[a-z]/.test(p)) classes++;
  if (/[A-Z]/.test(p)) classes++;
  if (/\d/.test(p)) classes++;
  if (/[^a-zA-Z0-9]/.test(p)) classes++;
  let score = 0;
  if (p.length >= 8) score++;
  if (p.length >= 12) score++;
  if (classes >= 3) score++;
  if (classes === 4 && p.length >= 14) score++;
  const labels = ["", "weak", "fair", "good", "strong"];
  const colors = [
    tokens.colorNeutralStroke2,
    "#C03A3A",
    "#A77B0E",
    "#3F8C3D",
    "#2E6B2C",
  ];
  return { score, label: labels[score], color: colors[score] };
}

export default function SignupPage() {
  const styles = useStyles();
  const router = useRouter();
  const signup = useSignup();

  const [workspace, setWorkspace] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [email, setEmail] = useState("");
  const [phoneCountry, setPhoneCountry] = useState("+1");
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [acceptTerms, setAcceptTerms] = useState(false);
  const [touched, setTouched] = useState<Record<keyof FieldErrors, boolean>>({
    workspace: false, firstName: false, lastName: false, email: false,
    phone: false, password: false, confirm: false, terms: false,
  });

  const strength = useMemo(() => passwordScore(password), [password]);

  const errors: FieldErrors = useMemo(() => {
    const e: FieldErrors = {};

    const wsTrim = workspace.trim();
    if (!wsTrim) e.workspace = "Workspace name is required";
    else if (wsTrim.length < 2) e.workspace = "At least 2 characters";
    else if (wsTrim.length > 64) e.workspace = "64 characters max";

    const fnTrim = firstName.trim();
    if (!fnTrim) e.firstName = "First name is required";
    else if (fnTrim.length > 64) e.firstName = "64 characters max";
    else if (!NAME_RE.test(fnTrim)) e.firstName = "Letters only";

    const lnTrim = lastName.trim();
    if (!lnTrim) e.lastName = "Last name is required";
    else if (lnTrim.length > 64) e.lastName = "64 characters max";
    else if (!NAME_RE.test(lnTrim)) e.lastName = "Letters only";

    const emTrim = email.trim();
    if (!emTrim) e.email = "Email is required";
    else if (emTrim.length > 256) e.email = "Email is too long";
    else if (!EMAIL_RE.test(emTrim)) e.email = "Enter a valid email address";

    // Phone is optional. If provided, validate digit count and that the
    // dial code is one we recognize (or at least matches +\d{1,4}).
    const phRaw = phone.trim();
    if (phRaw) {
      const d = phoneDigits(phRaw);
      if (d.length < 6) e.phone = "Too short — at least 6 digits";
      else if (d.length > 15) e.phone = "Too long — at most 15 digits";
      else if (!/^\+\d{1,4}$/.test(phoneCountry))
        e.phone = "Select a country code";
    }

    if (!password) e.password = "Password is required";
    else if (password.length < 8) e.password = "At least 8 characters";
    else if (password.length > 256) e.password = "Password is too long";
    else if (strength.score < 3)
      e.password = "Mix uppercase, lowercase, digits and symbols (3 of 4)";

    if (!confirm) e.confirm = "Confirm your password";
    else if (confirm !== password) e.confirm = "Passwords do not match";

    if (!acceptTerms) e.terms = "You must accept the terms";

    return e;
  }, [
    workspace, firstName, lastName, email, phone, phoneCountry,
    password, confirm, acceptTerms, strength.score,
  ]);

  const valid = Object.keys(errors).length === 0;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setTouched({
      workspace: true, firstName: true, lastName: true, email: true,
      phone: true, password: true, confirm: true, terms: true,
    });
    if (!valid) return;
    try {
      await signup.mutateAsync({
        workspace_name: workspace.trim(),
        first_name: firstName.trim(),
        last_name: lastName.trim(),
        email: email.trim().toLowerCase(),
        phone: phone.trim() ? phoneDigits(phone) : undefined,
        phone_country: phone.trim() ? phoneCountry : undefined,
        password,
        confirm_password: confirm,
        accept_terms: acceptTerms,
      });
      router.replace(`/check-email?email=${encodeURIComponent(email.trim())}`);
    } catch {
      /* server error renders below */
    }
  };

  const serverErr = signup.error as Error | null;
  const showErr = (k: keyof FieldErrors): string | undefined =>
    touched[k] ? errors[k] : undefined;

  return (
    <AuthShell
      title="Create your workspace"
      subtitle="Catalog, governance, and lineage — yours in under a minute."
    >
      <form className={styles.form} onSubmit={onSubmit} noValidate>
        <Field
          label="Workspace name"
          required
          validationState={showErr("workspace") ? "error" : "none"}
          validationMessage={showErr("workspace")}
        >
          <Input
            value={workspace}
            onChange={(_, d) => setWorkspace(d.value)}
            onBlur={() => setTouched((t) => ({ ...t, workspace: true }))}
            maxLength={64}
            disabled={signup.isPending}
          />
        </Field>

        <div className={styles.twoCol}>
          <Field
            label="First name"
            required
            validationState={showErr("firstName") ? "error" : "none"}
            validationMessage={showErr("firstName")}
          >
            <Input
              value={firstName}
              onChange={(_, d) => setFirstName(d.value)}
              onBlur={() => setTouched((t) => ({ ...t, firstName: true }))}
              maxLength={64}
              autoComplete="given-name"
              disabled={signup.isPending}
            />
          </Field>
          <Field
            label="Last name"
            required
            validationState={showErr("lastName") ? "error" : "none"}
            validationMessage={showErr("lastName")}
          >
            <Input
              value={lastName}
              onChange={(_, d) => setLastName(d.value)}
              onBlur={() => setTouched((t) => ({ ...t, lastName: true }))}
              maxLength={64}
              autoComplete="family-name"
              disabled={signup.isPending}
            />
          </Field>
        </div>

        <Field
          label="Work email"
          required
          validationState={showErr("email") ? "error" : "none"}
          validationMessage={showErr("email")}
        >
          <Input
            type="email"
            autoComplete="email"
            value={email}
            onChange={(_, d) => setEmail(d.value)}
            onBlur={() => setTouched((t) => ({ ...t, email: true }))}
            maxLength={256}
            disabled={signup.isPending}
          />
        </Field>

        <div className={styles.phoneBlock}>
          <Label htmlFor="phone-number" weight="semibold">
            Phone (optional)
          </Label>
          <div
            className={`${styles.phoneGroup} ${
              showErr("phone") ? styles.phoneGroupError : ""
            }`}
          >
            <span className={styles.phoneIcon} aria-hidden="true">
              <Call20Regular />
            </span>
            <select
              id="phone-country"
              aria-label="Country code"
              className={styles.phoneSelect}
              value={phoneCountry}
              onChange={(e) => setPhoneCountry(e.target.value)}
              disabled={signup.isPending}
            >
              {COUNTRY_CODES.map((c) => (
                <option key={c.code} value={c.code}>
                  {`${c.flag} ${c.code}  ${c.label}`}
                </option>
              ))}
            </select>
            <input
              id="phone-number"
              type="tel"
              inputMode="numeric"
              autoComplete="tel-national"
              placeholder="Phone number"
              className={styles.phoneNumberInput}
              value={phone}
              onChange={(e) => {
                // Strip everything but digits, spaces and dashes so the
                // field can never carry a duplicated dial code or letters.
                setPhone(e.target.value.replace(/[^\d\s-]/g, ""));
              }}
              onBlur={() => setTouched((t) => ({ ...t, phone: true }))}
              maxLength={20}
              disabled={signup.isPending}
            />
          </div>
          {showErr("phone") ? (
            <span className={styles.phoneError}>{showErr("phone")}</span>
          ) : (
            <span className={styles.phoneHint}>
              Used only for security alerts and break-glass recovery.
            </span>
          )}
        </div>

        <Field
          label="Password"
          required
          hint="8+ chars · 3 of: lowercase, uppercase, digit, symbol"
          validationState={showErr("password") ? "error" : "none"}
          validationMessage={showErr("password")}
        >
          <Input
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(_, d) => setPassword(d.value)}
            onBlur={() => setTouched((t) => ({ ...t, password: true }))}
            maxLength={256}
            disabled={signup.isPending}
          />
          {password && (
            <>
              <div className={styles.strengthBar}>
                <div
                  className={styles.strengthFill}
                  style={{
                    width: `${(strength.score / 4) * 100}%`,
                    backgroundColor: strength.color,
                  }}
                />
              </div>
              <span className={styles.hint}>{strength.label}</span>
            </>
          )}
        </Field>

        <Field
          label="Confirm password"
          required
          validationState={showErr("confirm") ? "error" : "none"}
          validationMessage={showErr("confirm")}
        >
          <Input
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(_, d) => setConfirm(d.value)}
            onBlur={() => setTouched((t) => ({ ...t, confirm: true }))}
            maxLength={256}
            disabled={signup.isPending}
          />
        </Field>

        <Checkbox
          label={
            <span>
              I agree to the <a href="/terms" className={styles.link}>terms</a> and{" "}
              <a href="/privacy" className={styles.link}>privacy policy</a>.
            </span>
          }
          checked={acceptTerms}
          onChange={(_, d) => {
            setAcceptTerms(!!d.checked);
            setTouched((t) => ({ ...t, terms: true }));
          }}
          disabled={signup.isPending}
        />
        {touched.terms && errors.terms && (
          <span style={{ color: tokens.colorPaletteRedForeground1, fontSize: 12 }}>
            {errors.terms}
          </span>
        )}

        {serverErr && (
          <MessageBar intent="error">
            <MessageBarBody>{serverErr.message}</MessageBarBody>
          </MessageBar>
        )}

        <Button
          type="submit"
          appearance="primary"
          size="large"
          disabled={signup.isPending || !valid}
        >
          {signup.isPending ? <Spinner size="tiny" /> : "Create workspace"}
        </Button>

        <div className={styles.meta}>
          Already have an account?{" "}
          <Link href="/login" className={styles.link}>
            Sign in
          </Link>
        </div>
      </form>
    </AuthShell>
  );
}
