# Testing AegisClaw

This document describes how to run, write, and maintain tests for AegisClaw. It complements the high-level standards in `docs/testing-standards.md` and the daemon lifecycle rules in `AGENTS.md`.

## Test Categories

| Type              | Command                        | Requires Daemon? | Purpose                                      | Notes |
|-------------------|--------------------------------|------------------|----------------------------------------------|-------|
| Unit              | `make test` or `go test ./...` | No               | Fast, isolated logic                         | Run on every change |
| Integration       | `make test-integration`        | Sometimes        | Daemon lifecycle, CLI, components            | Uses `-tags=integration` |
| E2E / Browser     | `make test-e2e` or `npm test`  | No (default)     | Web Portal UI + public REST contract         | Playwright (see below) |
| Smoke             | `make smoke`                   | Yes (`make start`) | Post-start sanity (CLI + portal reachability) | Quick health check |
| Full user journeys| (manual + Playwright)          | Yes              | End-to-end with real chat, Court, Builder    | See "Live E2E" section |

## Running Unit and Integration Tests

```bash
# All unit tests (no privileges needed)
make test

# Daemon-focused integration tests (build tag)
make test-integration
```

See `INTEGRATION_TESTS.md` and `TEST_EXECUTION_RESULTS.md` for historical results and the main integration test file (`cmd/aegis/daemon_integration_test.go`).

## E2E / Browser Tests (Playwright)

The project uses Playwright for Web Portal testing. Specs live in `e2e/`.

### Two Modes: Contract (Isolated) vs Live

**Default / Recommended for most work and CI:**

```bash
npm test
# or
make test-e2e
# or with UI
npx playwright test --headed
```

This starts the **thin web-portal binary in isolation** (via the `webServer` config in `playwright.config.js`). No daemon, no sudo, works on any machine with Go + Node.

- Uses the **E2E Fixture Client** (added 2026): when `AEGIS_SKILLS_FILE` / `AEGIS_PROPOSALS_FILE` (and `AEGIS_STORE_DATA_DIR`) are set, the portal loads the JSON fixtures from `cmd/web-portal/testdata/` and serves realistic data for:
  - `/api/skills`, `/api/proposals`, `/api/proposals/.../status`, etc.
  - `dashboard.skills` (powers the /skills page)
  - proposal create/list/get flows
  - basic approvals/court shapes
- This makes assertions like `toContainText('Discord Monitor')`, proposal lists, and REST contract tests pass reliably without a full stack.

**Live / Full System Journeys** (real chat streaming, tool calls, Court decisions, Builder flows, etc.):

1. Start the daemon (follow `AGENTS.md` exactly):
   ```bash
   make start   # or make start-foreground
   ```
2. Run Playwright pointed at the live proxy (default baseURL is `http://localhost:8080`, which the daemon reverse-proxies to the portal):
   ```bash
   npx playwright test e2e/ --headed
   ```
   Or temporarily override in a one-off run.

Many chat/streaming and deep interaction tests will only be meaningful (or pass) in live mode.

### Writing Good E2E Tests

- Prefer stable `data-testid` attributes (the dashboard templates and static shell have many; add more when introducing new UI).
- Be resilient to limited/fixture mode:
  - Use `if (res.status() === 201) { ... } else { expect(body.error).toContain('limited mode') ... }` for mutating calls.
  - Or check for either success data or graceful error shapes.
- Keep contract tests fast and deterministic (they run in CI on every PR).
- Heavy streaming / live-backend tests can live in the same files or be separated and guarded by an env var (`AEGIS_E2E_LIVE=1`).
- Use Playwright best practices: `getByTestId`, `getByRole`, explicit waits, `toBeVisible`, trace-on-first-retry (already configured).

### Configuration

- `playwright.config.js` — baseURL, projects (chromium/firefox/webkit), webServer command (the fixture invocation), CI retries/workers.
- `package.json` — `test` and `test:headed` scripts.
- Fixtures: `cmd/web-portal/testdata/skills.fixture.json` and `proposals.fixture.json`.
- The fixture client implementation lives in `cmd/web-portal/main.go` (`e2eFixtureClient` + `tryNewE2EFixtureClient`).

### Visual Regression / Screenshot Testing (Optional)

Playwright supports `expect(page).toHaveScreenshot()` for pixel-perfect (or thresholded) UI snapshots.

**6.7 Hardening note**: `e2e/snapshots/` + LFS patterns in `.gitattributes` are ready. Snapshot tests in `journeys.spec.js` are opt-in via `AEGIS_E2E_VISUAL=1` (skipped in normal CI/`npm test` to keep green without baselines committed). 

**If you add/enable screenshots:**

1. Store under `e2e/snapshots/` (configured via `snapshotDir` in playwright.config.js).
2. **You must use Git LFS** — browser screenshots are binary PNGs and will bloat the repo otherwise. The patterns are pre-tracked in `.gitattributes`.

Setup steps (run once per machine):

```bash
# Install Git LFS (platform-specific)
# macOS: brew install git-lfs
# Ubuntu/Debian: sudo apt-get install git-lfs
# Then:
git lfs install
```

To capture/update baselines (after code changes that affect UI):
```bash
AEGIS_E2E_VISUAL=1 npx playwright test -g "visual baseline" --update-snapshots
# Then: git add e2e/snapshots/ && git commit (LFS will handle the binaries)
```

See journeys.spec.js for the two starter visual tests (dashboard + skills). Add more for other key screens as journeys evolve.

Add (or ensure) the following in `.gitattributes` (create the file if it doesn't exist):

```
# E2E browser screenshots
e2e/snapshots/**/*.png filter=lfs diff=lfs merge=lfs -text
e2e/snapshots/**/*.jpg filter=lfs diff=lfs merge=lfs -text
```

Track new patterns as needed:

```bash
git lfs track "e2e/snapshots/**/*.png"
```

In CI, ensure the runner has Git LFS installed and the checkout step fetches LFS objects (most GitHub Actions `actions/checkout` + `git lfs` setup works out of the box after the attributes are committed).

Never commit large PNGs without LFS.

### Running Specific Tests

```bash
npx playwright test e2e/chat.spec.js
npx playwright test -g "User Journey 3"
```

See the Playwright docs for more filtering, debugging (`--debug`), and UI mode.

## Smoke Test

After `make start`:

```bash
make smoke
```

This is a fast, non-Playwright check that the daemon, reverse proxy, and key portal endpoints are reachable.

## Continuous Integration

The `.github/workflows/ci.yml` matrix includes:

- Unit + vet + build
- Lightweight integration
- Daemon integration tests (requires build)
- E2E (Node + Playwright browsers + `npm test` — runs in isolated fixture mode)
- Optional microVM image builds
- Opt-in heavy in-process tests (never on by default)

All required jobs must pass before merge.

## Updating Tests When Changing the Web Portal

1. If you add new UI elements → add `data-testid`.
2. If you change public REST shapes (`/api/proposals*`, etc.) → update both the Go handlers and the matching E2E expectations (and the fixture client responses if they affect contract mode).
3. If behavior changes only in full mode, make sure isolated tests still pass or are properly guarded.
4. Run `make test-e2e` locally before pushing.
5. Consider whether a new fixture entry or fixture client method is needed for good contract coverage.

## Related Documentation

- `docs/testing-standards.md` — high-level philosophy and coverage requirements.
- `AGENTS.md` — **mandatory** rules for starting/stopping the daemon (`make start` / `make stop`).
- `INTEGRATION_TESTS.md` — details on the daemon integration suite.
- `README.md` — quick start and high-level test commands.
- `e2e/` specs — living examples.
- `cmd/web-portal/main_test.go` — thin server contract tests (unit level).
- `playwright.config.js` and `cmd/web-portal/main.go` (fixture client section).

## Common Pitfalls

- Running E2E without the fixture env vars set (the `webServer` command in playwright.config does this for you).
- Assuming all tests can run without the daemon — only the contract slice can.
- Forgetting the exact daemon start/stop commands from `AGENTS.md` (sudo rules, etc.).
- Adding large screenshots without Git LFS.
- Mutating tests that don't handle the "limited mode" or fixture error responses gracefully.

Keep tests fast, deterministic, and honest about their requirements (isolated vs live). This is especially important for a security-sensitive project with privileged daemon components.
