# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: journeys.spec.js >> User Journey E2E Tests (expanded per docs/specs/user-journeys/ + web-portal.md) >> User Journey 9 (SDLC end-to-end skeleton): Full proposal → status → audit flow via thin portal REST (maps to journey 04/09 + web-portal e2e sdlc vision)
- Location: e2e/journeys.spec.js:104:7

# Error details

```
Error: expect(received).toBe(expected) // Object.is equality

Expected: 201
Received: 400
```

# Test source

```ts
  9   | 
  10  |     await page.goto('/#chat');
  11  |     await expect(page.getByTestId('chat-input')).toBeVisible();
  12  | 
  13  |     const input = page.getByTestId('chat-input');
  14  |     await input.fill('What is AegisClaw?');
  15  |     await page.getByTestId('chat-send-button').click();
  16  | 
  17  |     // Note: full streaming response requires live backend; UI feedback + input presence asserted
  18  |     await expect(page.locator('#chat-msgs, [data-testid="chat-messages"]')).toBeVisible();
  19  |   });
  20  | 
  21  |   test('User Journey 2+4: Skills discovery + Propose Skill button (journey 04)', async ({ page }) => {
  22  |     await page.goto('/');
  23  | 
  24  |     // Use stable nav + testid from data-testid sweep
  25  |     await page.getByTestId('nav-skills').click();
  26  |     await expect(page.getByTestId('propose-skill-button')).toBeVisible();
  27  | 
  28  |     // Proposals section (now has data-testid from server.go templates)
  29  |     await expect(page.getByTestId('proposals-section')).toBeVisible();
  30  |   });
  31  | 
  32  |   test('User Journey 3+4+6+9: Proposals list + detail via UI and documented public REST (web-portal.md contract)', async ({ page, request }) => {
  33  |     // 1. UI path (Skills/Proposals screen)
  34  |     await page.goto('/');
  35  |     await page.getByTestId('nav-skills').click();
  36  |     await expect(page.getByTestId('proposals-list')).toBeVisible();
  37  | 
  38  |     // 2. Exercise the exact public REST we implemented (thin delegation)
  39  |     const listRes = await request.get('/api/proposals');
  40  |     expect(listRes.ok()).toBeTruthy();
  41  |     const proposals = await listRes.json();
  42  |     expect(Array.isArray(proposals) || proposals !== null).toBeTruthy();
  43  | 
  44  |     // Create via documented POST /api/proposals (returns 201 + id)
  45  |     const createRes = await request.post('/api/proposals', {
  46  |       data: {
  47  |         title: 'E2E Test Skill from Playwright',
  48  |         description: 'Added during journey expansion test',
  49  |         permissions: ['fs.read']
  50  |       }
  51  |     });
  52  |     expect(createRes.status()).toBe(201);
  53  |     const created = await createRes.json();
  54  |     expect(created.id).toBeTruthy();
  55  |     const propId = created.id;
  56  | 
  57  |     // 3. Status endpoint (exact shape from spec)
  58  |     const statusRes = await request.get(`/api/proposals/${propId}/status`);
  59  |     expect(statusRes.ok()).toBeTruthy();
  60  |     const status = await statusRes.json();
  61  |     expect(status).toHaveProperty('phase');
  62  |     expect(status).toHaveProperty('court_approved');
  63  |     expect(status).toHaveProperty('code_generated');
  64  |     expect(status).toHaveProperty('pr_url');
  65  |     expect(status).toHaveProperty('deployed');
  66  |     expect(status).toHaveProperty('error');
  67  | 
  68  |     // 4. Audit endpoint (text/markdown per spec)
  69  |     const auditRes = await request.get(`/api/proposals/${propId}/audit`);
  70  |     expect(auditRes.ok()).toBeTruthy();
  71  |     const auditText = await auditRes.text();
  72  |     expect(auditText.length).toBeGreaterThan(10);
  73  |   });
  74  | 
  75  |   test('User Journey 6: Court decisions + approvals via new REST + UI (per journey 06 Success Criteria)', async ({ page, request }) => {
  76  |     await page.goto('/');
  77  |     await page.getByTestId('nav-court').click();
  78  | 
  79  |     // New documented endpoint
  80  |     const decisionsRes = await request.get('/api/court/decisions');
  81  |     expect(decisionsRes.ok()).toBeTruthy();
  82  | 
  83  |     // Approvals UI (data-testid added in sweep)
  84  |     await page.goto('/approvals');
  85  |     await expect(page.getByTestId('approvals-section')).toBeVisible();
  86  | 
  87  |     // Pending approvals list (if any in fixtures)
  88  |     const approvalsRes = await request.get('/api/approvals?pending=1');
  89  |     expect(approvalsRes.ok()).toBeTruthy();
  90  |     const approvals = await approvalsRes.json();
  91  |     expect(approvals !== undefined).toBeTruthy();
  92  |   });
  93  | 
  94  |   test('User Journey 5+8: Monitoring / Dashboard live stats + navigation (per journey 05)', async ({ page }) => {
  95  |     await page.goto('/');
  96  |     await expect(page.getByTestId('dashboard-stats')).toBeVisible();
  97  |     await expect(page.getByTestId('stat-running-vms')).toBeVisible();
  98  | 
  99  |     await page.getByTestId('nav-monitoring').click();
  100 |     // Monitoring panel / tasks would appear in full UI; assert nav worked
  101 |     await expect(page.getByTestId('nav-monitoring')).toHaveClass(/is-active|active/);
  102 |   });
  103 | 
  104 |   test('User Journey 9 (SDLC end-to-end skeleton): Full proposal → status → audit flow via thin portal REST (maps to journey 04/09 + web-portal e2e sdlc vision)', async ({ request }) => {
  105 |     // End-to-end slice using only the thin layer + fixtures (real Court/Builder would be live daemon)
  106 |     const create = await request.post('/api/proposals', {
  107 |       data: { title: 'Discord Monitor E2E', description: 'Journey 9 test skill' }
  108 |     });
> 109 |     expect(create.status()).toBe(201);
      |                             ^ Error: expect(received).toBe(expected) // Object.is equality
  110 |     const { id } = await create.json();
  111 | 
  112 |     const status = await (await request.get(`/api/proposals/${id}/status`)).json();
  113 |     expect(['review', 'pending', 'approved', 'unknown']).toContain(status.phase);
  114 | 
  115 |     const audit = await (await request.get(`/api/proposals/${id}/audit`)).text();
  116 |     expect(audit).toMatch(/Audit|proposal|Created|Court/i);
  117 |   });
  118 | });
  119 | 
```