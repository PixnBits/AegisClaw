# router.go

## Purpose
Maps Court reviewer personas to specific LLM model lists and inference parameters. Supports loading routing config from a single YAML file or auto-discovering persona YAML files in a directory.

## Key Types / Functions
- **`RouteMode`** – enum: `primary` (first model only), `fallback` (try in order), `ensemble` (run all in parallel).
- **`PersonaRoute`** – YAML-serialisable per-persona config: `Persona`, `Models`, `Temperature`, `Mode`, `OutputSchema`, `MaxTokens`.
- **`PersonaRoute.Validate()`** – checks required fields and range constraints.
- **`RouterConfig`** – top-level YAML struct with global defaults and a `Routes` slice.
- **`Router`** – runtime index; `routes map[string]*PersonaRoute`.
- **`NewRouter(cfg)`** – validates and indexes all routes; applies global defaults.
- **`LoadRouter(path)`** – reads and parses a YAML routing file.
- **`LoadRouterFromDir(dir)`** – scans `*.yaml` files, extracts `name`+`models` from each, builds a router with fallback defaults.
- **`Router.Resolve(persona)`** – returns a `ResolvedRoute` (with all zero-value fields filled from defaults); returns a default-only route for unknown personas.
- **`Router.Personas()`** / **`Router.HasPersona(persona)`** – introspection helpers.
- **`ResolvedRoute`** – fully resolved config; `PrimaryModel()` / `EnsembleModels()` helpers.

## System Role
Decouples persona logic from model selection. The Court pipeline queries the router before every inference call to determine which models to use, at what temperature, and in what mode.

## Notable Dependencies
- `gopkg.in/yaml.v3` – YAML parsing.
- `os` / `path/filepath` – file system traversal.
