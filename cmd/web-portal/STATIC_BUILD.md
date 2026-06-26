# Web Portal static build output

`static/` is **generated** by Vite. Do not commit `static/index.html` or `static/assets/`.

```bash
make build-web-portal
```

The guest binary and microVM image serve `static/` at runtime.