// playwright.config.js
import { defineConfig, devices } from '@playwright/test';
import fs from 'fs';

// Default: real system (daemon + microVMs on localhost:8080). Opt-in fixture via AEGIS_E2E_FIXTURE=1.
const fixtureMode = !!process.env.AEGIS_E2E_FIXTURE;
const useBinWebPortal =
  process.env.AEGIS_E2E_USE_BIN_WEBPORTAL === '1' ||
  (process.env.AEGIS_E2E_USE_BIN_WEBPORTAL !== '0' && fs.existsSync('bin/web-portal'));
const fixtureWebPortalCmd = useBinWebPortal
  ? 'AEGIS_E2E_FIXTURE=1 AEGIS_STORE_DATA_DIR=cmd/web-portal/testdata AEGIS_SKILLS_FILE=skills.fixture.json AEGIS_PROPOSALS_FILE=proposals.fixture.json ./bin/web-portal'
  : 'AEGIS_E2E_FIXTURE=1 AEGIS_STORE_DATA_DIR=cmd/web-portal/testdata AEGIS_SKILLS_FILE=skills.fixture.json AEGIS_PROPOSALS_FILE=proposals.fixture.json go run ./cmd/web-portal';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: !fixtureMode,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: fixtureMode ? undefined : 1,
  timeout: fixtureMode ? 30_000 : 240_000,
  reporter: 'html',
  use: {
    baseURL: 'http://localhost:8080',
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
          url: 'http://localhost:8080/health',
          reuseExistingServer: false,
          timeout: 45 * 1000,
        },
      }
    : {}),
  snapshotDir: './e2e/snapshots',
});
