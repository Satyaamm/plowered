"use client";

import { useEffect, useRef } from "react";
import { usePathname, useRouter } from "next/navigation";
import { driver, type Driver, type DriveStep } from "driver.js";
import "driver.js/dist/driver.css";
import { useCompleteTour } from "@/lib/auth-client";
import { usePrincipal } from "@/lib/hooks";

/**
 * Full product tour. Mounted once at the authed-layout level and
 * auto-fires on the first authenticated load when `tour_completed`
 * is still false. Also exposed via window.__plowered_tour so the
 * profile dropdown's "Take a tour" menu item can re-launch it.
 *
 * Targets DOM elements via `data-tour="<name>"` attributes added on
 * the sidebar items, topbar, and selected page surfaces. Each step
 * is intentionally verbose — the user explicitly asked the tour to
 * explain every feature in detail, not just label it.
 */

const STEPS: DriveStep[] = [
  {
    popover: {
      title: "Welcome to Plowered",
      description:
        "Plowered is the open-source data context platform: an Atlan / Collibra alternative your team self-hosts. " +
        "It catalogs every dataset, dashboard, and column, runs quality checks, orchestrates pipelines, " +
        "tracks lineage, and surfaces all of it to humans and AI agents through one API. " +
        "This tour walks you through every surface so you know where to look for what. " +
        "You can quit anytime with Esc, and re-run the tour later from the profile menu (top-right).",
    },
  },
  {
    element: '[data-tour="sidebar"]',
    popover: {
      title: "Sidebar — primary navigation",
      description:
        "Everything is one click away from this rail. Groups (General, Catalog, Orchestration, Data quality, " +
        "Governance, Compliance, Management) match the SOC 2 / GDPR control surface — your auditor sees the " +
        "same structure your data team does. Use the chevron at the bottom to collapse the rail when you " +
        "want more horizontal space.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-home"]',
    popover: {
      title: "Home — your daily dashboard",
      description:
        "The getting-started checklist appears here until you've wired your first connection. Below it: " +
        "live counts of catalog assets, active pipelines, alerts, audit events, and a feed of the most " +
        "recent requests against the API. Treat this as your situational-awareness pane.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-search"]',
    popover: {
      title: "Search — full-text + semantic",
      description:
        "Search every asset, pipeline, run, policy, and audit event from one box. The backend supports both " +
        "exact-match (for IDs and qualified names) and semantic similarity through the BYOM embeddings " +
        "provider you configure under AI providers. Press Enter in the topbar search to land here with " +
        "the query pre-filled.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-assets"]',
    popover: {
      title: "Catalog — every dataset Plowered knows about",
      description:
        "Tables, views, columns, dashboards, models. Each asset carries its qualified name, type, owners, " +
        "tags, classifications (PII / PHI / PCI / secret — auto-detected on crawl), lineage edges, and a " +
        "linked glossary term. Filter by type with the tabs at the top, click a row to drill into the asset " +
        "detail page with schema + lineage + quality + activity tabs.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-pipelines"]',
    popover: {
      title: "Pipelines — your DAG editor",
      description:
        "Author orchestrated jobs: connector syncs, SQL transforms, dbt runs, quality checks, webhooks. " +
        "Each pipeline is a DAG of typed tasks with explicit DependsOn edges. The editor catches cycles " +
        "client-side, validates cron expressions against the same parser the scheduler uses, and lets you " +
        "drag-connect dependencies. Triggers can be cron-scheduled or manual.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-runs"]',
    popover: {
      title: "Runs — pipeline execution history",
      description:
        "Every pipeline trigger lands here: status, duration, who/what triggered it, links to task-level " +
        "logs. The page auto-polls every 5 seconds while runs are in flight. Click a run to see the per-" +
        "task timeline, retry failed tasks, or tail the SSE log stream live.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-checks"]',
    popover: {
      title: "Quality checks — assertions on assets",
      description:
        "Row count thresholds, freshness windows, null-rate, uniqueness, custom SQL — declared once, run " +
        "on a schedule or before downstream tasks. Failed checks raise alerts through the channels you " +
        "configure under the Alerts page. Each check has a history view with a line chart so you can spot " +
        "trends, not just current state.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-alerts"]',
    popover: {
      title: "Alerts — notification routing",
      description:
        "Channels (Slack, email, webhooks) get wired to rules (which severities, which assets, which check " +
        "types). The deliveries log shows every notification we sent, with idempotency keys so a retried " +
        "pipeline doesn't double-page your on-call.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-policies"]',
    popover: {
      title: "Policies — fine-grained access control",
      description:
        "ABAC rules layered on top of workspace roles. Examples: 'only the finance group can read " +
        "tag:class:pii columns', or 'deny everyone delete on tag:critical assets'. Deny rules win over " +
        "allow. Every read and write checks against this policy table before the storage layer sees it.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-glossary"]',
    popover: {
      title: "Glossary — business term catalog",
      description:
        "Define your company's vocabulary (Revenue, ARR, MAU, Active Customer, …) and attach terms to " +
        "assets. The catalog then surfaces 'this column means X' to every consumer — humans and AI agents " +
        "alike. Terms can have parents to form a hierarchy.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-access"]',
    popover: {
      title: "Access — who can see what",
      description:
        "The read-side companion to Policies. Inspect any asset and see which roles/groups can read, " +
        "write, or delete it, with the exact policy rule that grants or denies each verb. Useful for " +
        "answering 'why can Alice see this and Bob can't?' without grepping the policy table.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-audit"]',
    popover: {
      title: "Audit log — every mutation, hash-chained",
      description:
        "Append-only record of every authenticated write across the platform: who, what, when, before/" +
        "after JSON, client IP, request id. Each row is SHA-256 chained to the prior row, so tampering " +
        "is detectable. Filter and export to CSV directly from this page. This is the SOC 2 CC8 evidence " +
        "your auditor will ask for.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-deleted"]',
    popover: {
      title: "Recycle bin — restore deleted records",
      description:
        "Every delete in Plowered is a tombstone, not a destructive operation. Browse what's been " +
        "removed, restore with one click, or — if you're a super_admin — purge permanently. Useful when " +
        "someone deletes the wrong pipeline at 2am.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-holds"]',
    popover: {
      title: "Legal holds — litigation gates",
      description:
        "Issue a hold against a resource type or specific IDs; from that moment, any delete attempt on a " +
        "held resource returns HTTP 409 with the hold reference. Required for e-discovery compliance. " +
        "Release the hold when counsel signs off, and deletes resume normally.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-dsr"]',
    popover: {
      title: "DSR requests — GDPR rights at scale",
      description:
        "Track Data Subject Requests (access, portability, rectification, erasure, restriction) end-to-" +
        "end with a 30-day statutory clock per request. Each filing is hash-chained into the audit log " +
        "so the regulator can see exactly when you received and completed it.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-connections"]',
    popover: {
      title: "Connections — your datasources",
      description:
        "Wire Postgres, Snowflake, BigQuery (and more) here. Credentials are sealed with AES-256-GCM " +
        "before they touch disk — Plowered itself can't read them after submission, only present them " +
        "to the connector at runtime. Every connection ships with Test (validates credentials), Crawl " +
        "(populates the catalog), and Classify (auto-tags PII/PHI columns).",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-team"]',
    popover: {
      title: "Team — invite teammates",
      description:
        "Invite by email with a role (viewer / editor / steward / admin). 7-day single-use invitation " +
        "links. Track pending invitations, change roles, remove members. Self-removal is blocked at the " +
        "API level so an admin can't accidentally lock themselves out.",
      side: "right",
    },
  },
  {
    element: '[data-tour="nav-ai"]',
    popover: {
      title: "AI providers — bring your own model",
      description:
        "Plowered is BYOM: you connect Anthropic, OpenAI, DeepSeek, or any OpenAI-compatible endpoint " +
        "with your own API key. Used for semantic search embeddings, glossary auto-write, and the " +
        "ClassifierAgent. The Test button validates the key with a tokenless probe before saving.",
      side: "right",
    },
  },
  {
    element: '[data-tour="topbar-search"]',
    popover: {
      title: "Global search",
      description:
        "Type anywhere and hit Enter to jump straight to the Search page with the query pre-filled. " +
        "Faster than navigating Sidebar → Search → typing.",
      side: "bottom",
    },
  },
  {
    element: '[data-tour="topbar-workspace"]',
    popover: {
      title: "Workspace switcher",
      description:
        "Shows the workspace you're currently signed into. If you belong to multiple workspaces, the " +
        "dropdown lets you re-authenticate into a different one — Plowered binds each session to a " +
        "single tenant for hard isolation.",
      side: "bottom",
    },
  },
  {
    element: '[data-tour="topbar-user"]',
    popover: {
      title: "Profile menu",
      description:
        "Account settings (profile, password, active sessions, GDPR export and erasure), workspace " +
        "links (team, connections, AI providers), API docs, and Sign out. You can also re-launch this " +
        "tour from here anytime via the 'Take the product tour' item.",
      side: "bottom",
    },
  },
  {
    popover: {
      title: "You're set",
      description:
        "That's the full tour. Re-run it anytime from the profile menu (top-right). " +
        "If you get stuck, the API docs are linked from the profile menu as well. Happy plowing.",
    },
  },
];

declare global {
  interface Window {
    __plowered_tour?: () => void;
  }
}

export function ProductTour() {
  const { principal, authenticated } = usePrincipal();
  const completeTour = useCompleteTour();
  const driverRef = useRef<Driver | null>(null);
  const router = useRouter();
  const pathname = usePathname();

  // Build the driver instance once; reuse across launches.
  useEffect(() => {
    const d = driver({
      showProgress: true,
      progressText: "Step {{current}} of {{total}}",
      animate: true,
      allowClose: true,
      overlayColor: "rgba(0,0,0,0.55)",
      popoverClass: "plowered-tour",
      nextBtnText: "Next →",
      prevBtnText: "← Back",
      doneBtnText: "Got it",
      steps: STEPS,
      onDestroyed: () => {
        // Whether the user clicked through to the end or hit Esc /
        // close, we treat it as "seen" — they can re-launch from
        // the profile menu if they want a refresher.
        completeTour.mutate();
      },
    });
    driverRef.current = d;

    // Expose a global so the profile menu (separate React tree under
    // the topbar) can fire the tour without prop-drilling.
    window.__plowered_tour = () => {
      if (pathname !== "/") {
        router.push("/");
        // Allow Next's route transition to settle before highlighting.
        setTimeout(() => d.drive(), 500);
      } else {
        d.drive();
      }
    };

    return () => {
      delete window.__plowered_tour;
      d.destroy();
    };
  }, [completeTour, pathname, router]);

  // Auto-launch on first authenticated load when the user hasn't
  // dismissed it yet. Only fire on the home route so the highlights
  // land on a predictable layout.
  useEffect(() => {
    if (!authenticated || !principal) return;
    if (principal.tourCompleted) return;
    if (pathname !== "/") return;
    const t = setTimeout(() => driverRef.current?.drive(), 800);
    return () => clearTimeout(t);
  }, [authenticated, principal, pathname]);

  return null;
}
