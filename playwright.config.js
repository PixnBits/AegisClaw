// playwright.config.js
import { defineConfig, devices } from '@playwright/test';
import fs from 'fs';

// Default: real system (daemon + microVMs on localhost:8080). Opt-in fixture via AEGIS_E2E_FIXTURE=1.
const fixtureMode = !!process.env.AEGIS_E2E_FIXTURE;
// Fixture tests bind a dedicated port so they always serve freshly built cmd/web-portal/static,
// even when the Host Daemon already occupies :8080 with a microVM image that may be stale.
const fixturePort = process.env.AEGIS_E2E_FIXTURE_PORT || '8090';
const fixtureBaseURL = `http://localhost:${fixturePort}`;
const useBinWebPortal =
  process.env.AEGIS_E2E_USE_BIN_WEBPORTAL === '1' ||
  (process.env.AEGIS_E2E_USE_BIN_WEBPORTAL !== '0' && fs.existsSync('bin/web-portal'));
const fixtureEnv = `AEGIS_E2E_FIXTURE=1 AEGIS_WEB_PORTAL_LISTEN_ADDR=:${fixturePort} AEGIS_STORE_DATA_DIR=cmd/web-portal/testdata AEGIS_SKILLS_FILE=skills.fixture.json AEGIS_PROPOSALS_FILE=proposals.fixture.json`;
const fixtureWebPortalCmd = useBinWebPortal
  ? `${fixtureEnv} ./bin/web-portal`
  : `${fixtureEnv} go run ./cmd/web-portal`;

export default defineConfig({
  testDir: './e2e',
  fullyParallel: !fixtureMode,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : fixtureMode ? 1 : 0,
  // Fixture tests share one in-memory channel/STOMP hub — parallel workers cause cross-test pollution.
  workers: 1,
  timeout: fixtureMode ? 30_000 : 240_000,
  reporter: 'html',
  use: {
    baseURL: fixtureMode ? fixtureBaseURL : 'http://localhost:8080',
    trace: 'on-first-retry',
  },
  projects: fixtureMode
    ? [
        { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
        { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
        { name: 'webkit', use: { ...devices['Desktop Safari'] } },
      ]
    : [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  ...(fixtureMode
    ? {
        webServer: {
          command: fixtureWebPortalCmd,
          url: `${fixtureBaseURL}/health`,
          reuseExistingServer: false,
          timeout: 45 * 1000,
        },
      }
    : {}),
  snapshotDir: './e2e/snapshots',
});
