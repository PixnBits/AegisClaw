# registry.go

## Purpose
Implements a read-only HTTP client for the ClawHub skill registry (`https://registry.clawhub.io`). The client allows the AegisClaw daemon to discover available skills, fetch their metadata, and retrieve full skill specifications. All imported skills must go through the Governance Court review process before execution; this client is the discovery mechanism only. Responses are capped at 512 KiB and all requests time out after 15 seconds.

## Key Types and Functions
- `Client`: HTTP client wrapping the registry base URL and a configured `http.Client`
- `NewClient(baseURL string) *Client`: constructs a client; defaults to `https://registry.clawhub.io`
- `ListSkills(ctx) ([]SkillEntry, error)`: fetches the full skill index from the registry
- `FetchSkill(ctx, name string) (*SkillEntry, error)`: fetches metadata for a named skill
- `FetchSkillSpec(ctx, name string) (*SkillSpec, error)`: fetches the full skill specification including entry point, network policy, tools, and capabilities
- `SkillEntry`: Name, Description, Version, Author, Risk, Tags, UpdatedAt
- `SkillSpec`: Name, Description, Language, EntryPoint, NetworkPolicy, SecretsRefs, Tools, Capabilities

## Role in the System
Used by the `aegisclaw import` CLI command and the Governance Court proposal wizard to discover and inspect skills available in the ClawHub registry. The registry client is strictly read-only; no skills are executed or stored until a proposal is approved.

## Dependencies
- `net/http`: HTTP client with 15-second timeout
- `encoding/json`: response deserialisation
- `io`: `LimitReader` for 512 KiB response cap
- `context`: request cancellation
