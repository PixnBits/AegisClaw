## Running Tests via Makefile

```bash
make test              # Normal fast tests (default in CI)
make test-integration  # Richer lifecycle & integration tests
make test-all          # Both
```

Integration tests use the `integration` build tag and are opt-in.