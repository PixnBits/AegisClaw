# `config/personas.yaml` — Summary

## Purpose

Central LLM routing configuration for the five Governance Court personas. Maps each persona name to one or more Ollama model names, inference temperature, routing mode, token budget, and the JSON output schema that every reviewer verdict must conform to.

## Key Fields

| Field | Description |
|---|---|
| `default_temperature` | Fallback temperature (0.7) when a persona route does not specify one |
| `default_mode` | Default routing strategy (`fallback`) |
| `default_max_tokens` | Default token budget (4096) |
| `routes[].persona` | One of: `CISO`, `SeniorCoder`, `SecurityArchitect`, `Tester`, `UserAdvocate` |
| `routes[].models` | Ordered list of Ollama model IDs; the routing mode determines how they are used |
| `routes[].temperature` | Per-persona inference temperature (lower = more deterministic) |
| `routes[].mode` | `ensemble` (aggregate multiple models) or `fallback` (try in order) |
| `routes[].output_schema` | Inline JSON schema requiring `verdict`, `risk_score`, `evidence`, `questions`, `comments` |

## Persona Routing Details

| Persona | Models | Temperature | Mode |
|---|---|---|---|
| CISO | mistral-nemo → llama3.2:3b | 0.3 | ensemble |
| SeniorCoder | mistral-nemo → llama3.2:3b | 0.5 | fallback |
| SecurityArchitect | mistral-nemo → llama3.2:3b | 0.4 | fallback |
| Tester | llama3.2:3b → mistral-nemo | 0.6 | fallback |
| UserAdvocate | llama3.2:3b → mistral-nemo | 0.7 | fallback |

## Fit in the Broader System

Loaded by `internal/llm` at daemon startup and consumed by `internal/court` when dispatching review tasks to sandboxed reviewer microVMs. Each reviewer VM uses its persona route to select the correct model and enforce structured output. The CISO uses ensemble mode (highest rigour); other personas use fallback (cost-efficient).

## Notable Dependencies

- Ollama (local inference server) must have `mistral-nemo` and `llama3.2:3b` pulled.
- Companion persona definitions live in `config/personas/*.yaml`.
- Output schema must match the `Verdict` struct in `internal/court`.
