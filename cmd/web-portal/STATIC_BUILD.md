# Web Portal static build output

`static/` is **generated** by Vite and must not be committed. Each build produces:

- `index.html` — references the current hashed JS/CSS bundles (cache busting)
- `assets/*` — bundled application code

## Local development / fixture E2E

```bash
make build-web-portal
```

`make build-binaries` and `make test-e2e-contract` run this automatically.

## microVM image

The web-portal `Dockerfile` runs `npm run build` in a Node stage before copying `/static`
into the guest image. Hashed asset names stay in sync with `index.html` at image build time.

`make build` → `build-binaries` → `build-web-portal` still refreshes host-side static for
`bin/web-portal` and Playwright fixture mode before `build-microvms` packages images.