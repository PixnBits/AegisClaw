// playwright.config.js
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    baseURL: 'http://localhost:8080',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'firefox',
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'webkit',
      use: { ...devices['Desktop Safari'] },
    },
  ],
  webServer: {
    // Starts the thin web-portal in E2E fixture mode (loads skills/proposals fixtures into
    // an in-memory mock client). This lets contract + UI shell + public REST tests run
    // reliably in CI and dev without a full daemon/Hub. See cmd/web-portal/main.go.
    command: 'AEGIS_STORE_DATA_DIR=cmd/web-portal/testdata AEGIS_SKILLS_FILE=skills.fixture.json AEGIS_PROPOSALS_FILE=proposals.fixture.json go run ./cmd/web-portal',
    url: 'http://localhost:8080/health',
    reuseExistingServer: !process.env.CI,
    timeout: 45 * 1000,
  },
  // 6.7 hardening: snapshots under e2e/snapshots per TESTING.md + .gitattributes LFS rules.
  // Use AEGIS_E2E_VISUAL=1 to enable toHaveScreenshot assertions (otherwise skipped).
  // First baseline: npx playwright test -g "visual" --update-snapshots, then git add + LFS.
  snapshotDir: './e2e/snapshots',
});
