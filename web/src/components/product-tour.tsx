"use client";

import { useEffect, useRef } from "react";
import { usePathname, useRouter } from "next/navigation";
import { driver, type Driver } from "driver.js";
import "driver.js/dist/driver.css";
import "./product-tour.css";
import { useCompleteTour } from "@/lib/auth-client";
import { usePrincipal } from "@/lib/hooks";

/**
 * Full product demo. Unlike a typical highlight-only tour, this one
 * actually navigates between pages so the user sees each surface in
 * its real context. The state machine lives in showStep(): for every
 * step we route to the right page first, wait one tick for the
 * Next.js render to settle, then ask driver.js to highlight the
 * relevant element (or, when no element is provided, show a centered
 * popover over the page).
 *
 * Auto-advance fires every 6 seconds. Hovering the popover pauses
 * the timer so people who want to read longer can. Next / Back /
 * Skip tour are always visible in the popover footer.
 */

const AUTO_ADVANCE_MS = 6000;

type Step = {
  route?: string;                // page to navigate to (omit = stay on current page)
  element?: string;              // CSS selector to spotlight (omit = centered popover)
  title: string;
  description: string;
  side?: "top" | "bottom" | "left" | "right";
};

const STEPS: Step[] = [
  // ---- Intro on Home ----
  {
    route: "/",
    title: "Welcome to Plowered",
    description:
      "Plowered is the open-source data context platform — an Atlan / Collibra alternative your team " +
      "self-hosts. It catalogs every dataset, runs quality checks, orchestrates pipelines, tracks " +
      "lineage, and surfaces all of it to humans and AI agents through one API. This guided demo will " +
      "walk you through every page so you know exactly where to look for what. Each step auto-advances " +
      "in 6 seconds — hover the popover to pause, or use Back / Next / Skip tour.",
  },
  {
    route: "/",
    element: '[data-tour="sidebar"]',
    side: "right",
    title: "Sidebar — primary navigation",
    description:
      "Every module is one click from this rail, grouped by purpose: General, Catalog, Orchestration, " +
      "Data Quality, Governance, Compliance, Management. The grouping mirrors the SOC 2 / GDPR control " +
      "surface — your auditor sees the same structure your data team does.",
  },
  {
    route: "/",
    element: '[data-tour="topbar-search"]',
    side: "bottom",
    title: "Global search",
    description:
      "Type anywhere and hit Enter to jump to results. Backed by full-text plus semantic similarity " +
      "(via the BYOM embeddings provider you configure under AI providers). Searches assets, " +
      "pipelines, runs, policies, and audit events in one box.",
  },
  {
    route: "/",
    element: '[data-tour="topbar-workspace"]',
    side: "bottom",
    title: "Workspace switcher",
    description:
      "Shows the workspace you're in. If you belong to multiple workspaces, the dropdown lets you " +
      "re-authenticate into another — Plowered binds each session to exactly one tenant for hard " +
      "isolation.",
  },
  // ---- Catalog ----
  {
    route: "/catalog",
    title: "Catalog — every dataset Plowered knows about",
    description:
      "Tables, views, columns, dashboards, models. Each asset carries qualified name, type, owners, " +
      "tags, classifications (PII / PHI / PCI / secret — auto-detected on crawl), lineage edges, and " +
      "linked glossary terms. Use the tabs to filter by type, click a row to drill into Schema / " +
      "Lineage / Quality / Activity tabs on the detail page.",
  },
  // ---- Pipelines ----
  {
    route: "/pipelines",
    title: "Pipelines — your DAG orchestrator",
    description:
      "Author orchestrated jobs: connector syncs, SQL transforms, dbt runs, quality checks, webhooks. " +
      "Each pipeline is a typed DAG with explicit DependsOn edges; the editor catches cycles client-" +
      "side and validates cron expressions against the same parser the scheduler uses.",
  },
  // ---- Runs ----
  {
    route: "/runs",
    title: "Runs — execution history",
    description:
      "Every pipeline trigger lands here with status, duration, who/what triggered it, and links to " +
      "task-level logs. Auto-polls every 5 seconds while runs are in flight. Click a run to see the " +
      "per-task timeline and tail the SSE log stream live.",
  },
  // ---- Quality checks ----
  {
    route: "/checks",
    title: "Quality checks — assertions on assets",
    description:
      "Row count thresholds, freshness windows, null-rate, uniqueness, custom SQL. Declare once, run " +
      "on a schedule or before downstream tasks. Failed checks raise alerts through the channels " +
      "configured under Alerts. Each check has a history view with a line chart so you spot trends.",
  },
  // ---- Alerts ----
  {
    route: "/alerts",
    title: "Alerts — notification routing",
    description:
      "Channels (Slack, email, webhooks) get wired to rules (which severities, which assets, which " +
      "check types). The deliveries log shows every notification we sent with idempotency keys so a " +
      "retried pipeline can't double-page your on-call.",
  },
  // ---- Governance ----
  {
    route: "/admin/policies",
    title: "Policies — fine-grained ABAC",
    description:
      "Attribute-based access rules layered on top of workspace roles. Examples: 'only the finance " +
      "group can read tag:class:pii columns', or 'deny everyone delete on tag:critical assets'. Deny " +
      "rules override allow. Every read and write checks against this table before the storage layer.",
  },
  {
    route: "/glossary",
    title: "Glossary — business term catalog",
    description:
      "Define your company's vocabulary (Revenue, ARR, MAU, Active Customer) and attach terms to " +
      "assets. The catalog then surfaces 'this column means X' to humans and AI agents alike. Terms " +
      "support parent-child hierarchies.",
  },
  {
    route: "/access",
    title: "Access — who can see what",
    description:
      "Read-side companion to Policies. Inspect any asset, see exactly which roles/groups can read, " +
      "write, or delete it, and which rule grants or denies each verb. Useful for answering 'why can " +
      "Alice see this and Bob can't?' without grepping the policy table.",
  },
  // ---- Compliance ----
  {
    route: "/admin/audit",
    title: "Audit log — hash-chained mutations",
    description:
      "Append-only record of every authenticated write: who, what, when, before/after JSON, IP, " +
      "request id. Each row is SHA-256 chained to the prior row, so tampering is detectable. Filter " +
      "and export to CSV. This is the SOC 2 CC8 evidence your auditor asks for.",
  },
  {
    route: "/admin/deleted",
    title: "Recycle bin — undo deletions",
    description:
      "Every delete is a tombstone, not a destructive operation. Browse what's been removed, restore " +
      "with one click, or — as a super_admin — purge permanently. Useful when someone deletes the " +
      "wrong pipeline at 2am.",
  },
  {
    route: "/legal-holds",
    title: "Legal holds — litigation gates",
    description:
      "Issue a hold against a resource type or specific IDs. Until released, any delete attempt on a " +
      "held resource returns HTTP 409 with the hold reference. Required for e-discovery compliance. " +
      "Release when counsel signs off and deletes resume normally.",
  },
  {
    route: "/dsr",
    title: "DSR requests — GDPR rights at scale",
    description:
      "Track Data Subject Requests (access, portability, rectification, erasure, restriction) end-to-" +
      "end with a 30-day statutory clock per request. Each filing is hash-chained into the audit log " +
      "so the regulator can see exactly when you received and completed it.",
  },
  // ---- Management ----
  {
    route: "/connections",
    title: "Connections — your datasources",
    description:
      "Wire Postgres, Snowflake, BigQuery, and more here. Credentials are sealed with AES-256-GCM " +
      "before they touch disk; Plowered itself can't read them, only present them to the connector at " +
      "runtime. Each connection ships with Test (validates creds), Crawl (populates the catalog), " +
      "and Classify (auto-tags PII / PHI columns).",
  },
  {
    route: "/team",
    title: "Team — invite teammates",
    description:
      "Invite by email with a role (viewer / editor / steward / admin). 7-day single-use invitation " +
      "links. Manage pending invites, change roles, remove members. Self-removal is blocked at the " +
      "API so an admin can't accidentally lock themselves out.",
  },
  {
    route: "/settings/ai",
    title: "AI providers — bring your own model",
    description:
      "Plowered is BYOM — connect Anthropic, OpenAI, DeepSeek, or any OpenAI-compatible endpoint " +
      "with your own API key. Used for semantic search embeddings, glossary auto-write, and the " +
      "ClassifierAgent. The Test button validates the key with a tokenless probe before saving.",
  },
  {
    route: "/account",
    title: "Account settings — your profile",
    description:
      "Update your name and phone, change your password (rotating revokes every active session), " +
      "review your active sessions and sign out remotely from any device. The GDPR data export and " +
      "self-service erasure also live here under the Account tab.",
  },
  // ---- Profile menu finale ----
  {
    route: "/",
    element: '[data-tour="topbar-user"]',
    side: "bottom",
    title: "Profile menu — your account hub",
    description:
      "Account settings, workspace links (team, connections, AI providers), API docs, and Sign out. " +
      "You can re-launch this tour anytime via the 'Take the product tour' entry in this menu.",
  },
  {
    route: "/",
    title: "You're set",
    description:
      "That's every page in Plowered. Re-run this tour anytime from the profile menu, and check the " +
      "API docs (also in that menu) for everything you can drive from a terminal or CI pipeline. " +
      "Happy plowing.",
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
  const router = useRouter();
  const pathname = usePathname();
  const driverRef = useRef<Driver | null>(null);
  const advanceTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const stepIndex = useRef(0);
  const completedRef = useRef(false);

  useEffect(() => {
    const clearAdvance = () => {
      if (advanceTimer.current) {
        clearTimeout(advanceTimer.current);
        advanceTimer.current = null;
      }
    };

    const finish = () => {
      clearAdvance();
      if (completedRef.current) return;
      completedRef.current = true;
      completeTour.mutate();
      driverRef.current?.destroy();
    };

    const armAdvance = () => {
      clearAdvance();
      advanceTimer.current = setTimeout(() => {
        if (stepIndex.current >= STEPS.length - 1) {
          finish();
        } else {
          stepIndex.current += 1;
          showStep(stepIndex.current);
        }
      }, AUTO_ADVANCE_MS);
    };

    const decoratePopover = () => {
      const popover = document.querySelector<HTMLElement>(".driver-popover");
      if (!popover) return;

      // Floating progress bar pinned to the bottom edge of the popover.
      // The CSS keyframe fills 0 → 100% over AUTO_ADVANCE_MS so the
      // visual stays in lock-step with the timer.
      if (!popover.querySelector(".plowered-tour-bar")) {
        const bar = document.createElement("div");
        bar.className = "plowered-tour-bar";
        bar.innerHTML = '<div class="plowered-tour-bar-fill"></div>';
        popover.appendChild(bar);
      }
      // Force the fill animation to restart on each step.
      const fill = popover.querySelector<HTMLElement>(".plowered-tour-bar-fill");
      if (fill) {
        fill.style.animation = "none";
        void fill.offsetWidth;
        fill.style.animation = `plowered-tour-fill ${AUTO_ADVANCE_MS}ms linear forwards`;
      }

      // Skip-tour button in the footer (left side).
      const footer = popover.querySelector(".driver-popover-footer");
      if (footer && !footer.querySelector(".plowered-tour-skip")) {
        const skip = document.createElement("button");
        skip.className = "plowered-tour-skip";
        skip.type = "button";
        skip.textContent = "Skip tour";
        skip.addEventListener("click", finish);
        footer.insertBefore(skip, footer.firstChild);
      }

      // Pause on hover.
      popover.addEventListener("mouseenter", () => {
        clearAdvance();
        if (fill) fill.style.animationPlayState = "paused";
      });
      popover.addEventListener("mouseleave", () => {
        if (fill) fill.style.animationPlayState = "running";
        armAdvance();
      });
    };

    const showStep = (i: number) => {
      if (i < 0 || i >= STEPS.length) return;
      const step = STEPS[i];
      stepIndex.current = i;
      clearAdvance();

      const render = () => {
        // driver.js v1's highlight() shows one step; we manage
        // next/prev/close ourselves so we can also route between
        // pages and pre-load the next surface.
        driverRef.current?.highlight({
          element: step.element,
          popover: {
            title: step.title,
            description: step.description,
            side: step.side,
            showButtons: ["next", "previous", "close"],
            progressText: `Step ${i + 1} of ${STEPS.length}`,
            onNextClick: () => {
              if (i >= STEPS.length - 1) {
                finish();
              } else {
                showStep(i + 1);
              }
            },
            onPrevClick: () => {
              if (i > 0) showStep(i - 1);
            },
            onCloseClick: finish,
          },
        });
        // driver.js renders synchronously; decorate on the next tick.
        requestAnimationFrame(() => {
          decoratePopover();
          armAdvance();
        });
      };

      if (step.route && step.route !== window.location.pathname) {
        router.push(step.route);
        // Give Next.js a beat to mount the destination page before
        // we try to query its DOM for the spotlight target.
        setTimeout(render, 700);
      } else {
        render();
      }
    };

    const d = driver({
      animate: true,
      allowClose: true,
      overlayColor: "rgba(15,23,42,0.55)",
      popoverClass: "plowered-tour",
      nextBtnText: "Next →",
      prevBtnText: "← Back",
      doneBtnText: "Done",
      onDestroyed: () => {
        clearAdvance();
        if (!completedRef.current) {
          completedRef.current = true;
          completeTour.mutate();
        }
      },
    });
    driverRef.current = d;

    window.__plowered_tour = () => {
      completedRef.current = false;
      stepIndex.current = 0;
      showStep(0);
    };

    return () => {
      clearAdvance();
      delete window.__plowered_tour;
      d.destroy();
    };
  }, [completeTour, router]);

  // Auto-launch on first authenticated load when the user hasn't
  // dismissed it yet. Only fire on home so the first highlight lands
  // on a predictable layout.
  useEffect(() => {
    if (!authenticated || !principal) return;
    if (principal.tourCompleted) return;
    if (pathname !== "/") return;
    const t = setTimeout(() => {
      stepIndex.current = 0;
      window.__plowered_tour?.();
    }, 900);
    return () => clearTimeout(t);
  }, [authenticated, principal, pathname]);

  return null;
}
