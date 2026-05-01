# router_test.go

## Purpose
Tests for the `Router` type and related helpers, including construction, persona resolution, config loading from YAML files and directories, and validation edge cases.

## Key Test Cases
- **`TestNewRouter`** – constructs a router with two routes and verifies `HasPersona` results.
- **`TestNewRouterValidation`** – rejects empty persona name, empty models list, invalid mode string, and out-of-range temperature.
- **`TestRouterResolve`** – explicit route values are returned; non-zero `MaxTokens` and multi-model slice preserved.
- **`TestRouterResolveDefaults`** – zero-value temperature/mode/maxTokens in a route fall back to global defaults.
- **`TestRouterResolveUnknown`** – unknown persona returns a route with the default params and an empty models slice.
- **`TestResolvedRoutePrimaryModel`** – returns first model or empty string.
- **`TestResolvedRouteEnsembleModels`** – returns all models in ensemble mode; only first in fallback/primary.
- **`TestRouterPersonas`** – correct count of configured persona names.
- **`TestLoadRouterFromFile`** – full round-trip: write YAML to temp dir, load, resolve, verify temperature and mode.
- **`TestLoadRouterFromDir`** – discovers two persona files, skips non-YAML; CISO route carries `OutputSchema`.
- **`TestLoadRouterFromDirSkipsNonYAML`** – `.txt` file is ignored.
- **`TestPersonaRouteValidate`** – valid route and zero-temperature both pass.
- **`TestLoadRouterMissingFile`** / **`TestLoadRouterInvalidYAML`** – error propagation for bad inputs.

## Notable Dependencies
- `os`, `path/filepath` – temp-dir file creation for YAML loading tests.
