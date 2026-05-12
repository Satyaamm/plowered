import { test, expect, Page } from "@playwright/test";

/**
 * PurpleCube AI Studio — full product walkthrough.
 *
 * Drives Chromium through every major surface so the recorded video
 * shows what the product can do end-to-end. Every section is wrapped
 * in a guard that swallows errors so a single missing selector never
 * kills the whole recording.
 *
 * Run:
 *   DEMO_EMAIL=... DEMO_PASSWORD=... npm run demo:headless
 * The .webm lands in ./demo-output/.
 */

const EMAIL = process.env.DEMO_EMAIL ?? "satyam.pathak@s2datasystems.in";
const PASSWORD = process.env.DEMO_PASSWORD ?? "";

const beat = async (page: Page, ms = 1500) => page.waitForTimeout(ms);

const section = async (name: string, fn: () => Promise<void>) => {
  try {
    await fn();
  } catch (e) {
    console.log(`[demo] section "${name}" continued past error:`, (e as Error).message);
  }
};

const tryClick = async (page: Page, selector: string, ms = 1200) => {
  try {
    const el = page.locator(selector).first();
    if ((await el.count()) === 0) return false;
    await el.scrollIntoViewIfNeeded().catch(() => {});
    await el.hover().catch(() => {});
    await beat(page, 400);
    await el.click({ timeout: 5_000 }).catch(() => {});
    await beat(page, ms);
    return true;
  } catch {
    return false;
  }
};

const visit = async (page: Page, path: string, ms = 3000) => {
  await page.goto(path).catch(() => {});
  await beat(page, ms);
};

test("PurpleCube full walkthrough", async ({ page }) => {
  test.setTimeout(10 * 60 * 1000);

  // ---------------------------------------------------------------
  // 1. Login
  // ---------------------------------------------------------------
  await section("login", async () => {
    await visit(page, "/login", 2500);
    await page.locator('input[type="email"]').first().fill(EMAIL);
    await beat(page, 700);
    if (PASSWORD) {
      await page.locator('input[type="password"]').first().fill(PASSWORD);
      await beat(page, 900);
      await page.getByRole("button", { name: /sign in/i }).click().catch(() => {});
      await page
        .waitForURL((u) => !u.pathname.startsWith("/login"), { timeout: 20_000 })
        .catch(() => {});
    } else {
      await visit(page, "/signup", 3000);
    }
  });

  // ---------------------------------------------------------------
  // 2. Home dashboard
  // ---------------------------------------------------------------
  await section("home", async () => {
    await visit(page, "/", 3500);
    await tryClick(page, '[data-tour="topbar-workspace"]', 1800);
    await page.keyboard.press("Escape").catch(() => {});
    await beat(page, 600);
    const search = page.locator('[data-tour="topbar-search"]').first();
    if (await search.count()) {
      await search.click().catch(() => {});
      await search.fill("orders").catch(() => {});
      await beat(page, 1500);
      await search.fill("").catch(() => {});
      await page.keyboard.press("Escape").catch(() => {});
    }
  });

  // ---------------------------------------------------------------
  // 3. Catalog → asset detail tabs
  // ---------------------------------------------------------------
  await section("catalog", async () => {
    await visit(page, "/catalog", 3500);
    const catSearch = page.getByPlaceholder(/search/i).first();
    if (await catSearch.count()) {
      await catSearch.click();
      await catSearch.fill("email");
      await beat(page, 2500);
    }
    const firstRow = page.locator('a[href^="/asset/"]').first();
    if (await firstRow.count()) {
      await firstRow.click().catch(() => {});
      await beat(page, 3500);
      for (const tab of ["Schema", "Lineage", "Quality", "Activity"]) {
        await tryClick(page, `role=tab[name="${tab}"]`, 2000);
      }
    }
    await page.goBack().catch(() => {});
    await beat(page, 1500);
  });

  // ---------------------------------------------------------------
  // 4. Connections — wizard
  // ---------------------------------------------------------------
  await section("connections", async () => {
    await visit(page, "/connections", 3000);
    await tryClick(page, 'button:has-text("New connection")', 2500);
    await tryClick(page, 'button:has-text("Cancel")', 1200);
    await page.keyboard.press("Escape").catch(() => {});
  });

  // ---------------------------------------------------------------
  // 5. Pipelines — list + new pipeline form (CronPicker + DAG)
  // ---------------------------------------------------------------
  await section("pipelines-list", async () => {
    await visit(page, "/pipelines", 3000);
  });

  await section("pipelines-new", async () => {
    await visit(page, "/pipelines/new", 3500);

    const nameField = page.getByPlaceholder(/nightly-orders/i).first();
    if (await nameField.count()) {
      await nameField.click();
      await nameField.type("demo-revenue-rollup", { delay: 60 });
      await beat(page, 900);
    }

    // Cycle Schedule frequency: Daily → Weekly → Hourly to show options
    const allDropdowns = page.getByRole("combobox");
    if ((await allDropdowns.count()) > 0) {
      await allDropdowns.first().click().catch(() => {});
      await beat(page, 1500);
      await page.getByRole("option", { name: /weekly/i }).click({ timeout: 4000 }).catch(() => {});
      await beat(page, 1500);

      await allDropdowns.first().click().catch(() => {});
      await beat(page, 1200);
      await page.getByRole("option", { name: /hourly/i }).click({ timeout: 4000 }).catch(() => {});
      await beat(page, 1500);

      await allDropdowns.first().click().catch(() => {});
      await beat(page, 1200);
      await page.getByRole("option", { name: /daily/i }).click({ timeout: 4000 }).catch(() => {});
      await beat(page, 1500);
    }

    // Add tasks to the DAG palette
    await tryClick(page, 'button:has-text("SQL")', 1500);
    await tryClick(page, 'button:has-text("Transform")', 1500);
    await tryClick(page, 'button:has-text("Quality check")', 1500);
    await beat(page, 1500);

    // Flip Config mode and click a node
    const configSwitch = page.getByRole("switch", { name: /config mode/i }).first();
    if (await configSwitch.count()) {
      await configSwitch.click().catch(() => {});
      await beat(page, 1300);
      const node = page.locator(".react-flow__node").first();
      if (await node.count()) {
        await node.click().catch(() => {});
        await beat(page, 2200);
        await page.keyboard.press("Escape").catch(() => {});
      }
    }
  });

  // ---------------------------------------------------------------
  // 6. Runs
  // ---------------------------------------------------------------
  await section("runs", async () => visit(page, "/runs", 3500));

  // ---------------------------------------------------------------
  // 7. Quality — Checks + Alerts
  // ---------------------------------------------------------------
  await section("checks", async () => visit(page, "/checks", 3000));
  await section("alerts", async () => visit(page, "/alerts", 3000));

  // ---------------------------------------------------------------
  // 8. Governance — Policies, Glossary, Access
  // ---------------------------------------------------------------
  await section("policies", async () => visit(page, "/admin/policies", 3500));
  await section("glossary", async () => visit(page, "/glossary", 3000));
  await section("access", async () => visit(page, "/access", 3500));

  // ---------------------------------------------------------------
  // 9. Compliance — Audit, Recycle bin, Legal holds, DSR
  // ---------------------------------------------------------------
  await section("audit", async () => visit(page, "/admin/audit", 3500));
  await section("recycle-bin", async () => visit(page, "/admin/deleted", 3000));
  await section("legal-holds", async () => visit(page, "/legal-holds", 3000));
  await section("dsr", async () => visit(page, "/dsr", 3000));

  // ---------------------------------------------------------------
  // 10. Management — Team, AI providers, Account
  // ---------------------------------------------------------------
  await section("team", async () => visit(page, "/team", 3000));
  await section("ai-providers", async () => visit(page, "/settings/ai", 3000));

  await section("account", async () => {
    await visit(page, "/account", 3000);
    for (const t of ["Password", "Active sessions", "Profile"]) {
      await tryClick(page, `role=tab[name="${t}"]`, 2200);
    }
  });

  // ---------------------------------------------------------------
  // 11. Profile-menu finale
  // ---------------------------------------------------------------
  await section("profile-menu", async () => {
    await visit(page, "/", 2000);
    await tryClick(page, '[data-tour="topbar-user"]', 2500);
    await page.keyboard.press("Escape").catch(() => {});
    await beat(page, 2500);
  });

  // Hold the final frame.
  await beat(page, 2500);

  // No-op assertion — this test exists purely to produce a video.
  expect(true).toBe(true);
});
