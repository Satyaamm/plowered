import { defineConfig } from "@playwright/test";

// Demo-recording config: headed Chromium, fixed 1440x900 viewport,
// video saved to ./demo-output/ as WebM. Convert to MP4 with ffmpeg if
// needed: `ffmpeg -i demo.webm -c:v libx264 -crf 23 demo.mp4`.
export default defineConfig({
  testDir: "./demo",
  // Single worker, no parallelism — we want one clean recording.
  workers: 1,
  // The demo is long; let it run for up to 10 minutes.
  timeout: 10 * 60 * 1000,
  expect: { timeout: 15_000 },
  reporter: [["list"]],
  use: {
    baseURL: process.env.DEMO_BASE_URL ?? "http://localhost:3000",
    viewport: { width: 1440, height: 900 },
    video: {
      mode: "on",
      size: { width: 1440, height: 900 },
    },
    // Slow each action down a touch so the cursor motion is readable
    // on playback.
    actionTimeout: 12_000,
    navigationTimeout: 20_000,
    launchOptions: {
      slowMo: 220,
      args: ["--window-size=1440,900"],
    },
  },
  outputDir: "./demo-output",
});
