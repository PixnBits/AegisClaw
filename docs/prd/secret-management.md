# Secret Management

Secrets must never be visible to the LLM, agent prompts, logs, or skill code.

## Core Principle

No secret may ever enter an LLM prompt or the memory of any agent or skill microVM.

## Architecture

A dedicated **Network Boundary VM** is the only component allowed to handle secrets. This VM runs **Envoy** as a sidecar proxy for all outbound traffic.

## Network Access Declaration

When a new skill is proposed, it must include a strict, declarative `network-access.yaml` file as part of the Court proposal.

This declaration defines:

- Which hosts and ports the skill may contact
- What authentication method to use (bearer, oauth2, api-key, custom header, etc.)
- Which secrets are required (by reference only — never the actual value)
- Allowed HTTP methods and headers

**Example:**

```yaml
network:
  endpoints:
    - name: discord-api
      host: discord.com
      port: 443
      auth:
        type: bearer
        secret_ref: discord_bot_token
      allowed_methods: allowed_headers: ```

The Governance Court reviews this declaration. Only if approved does the Network Boundary VM generate the corresponding Envoy configuration.

## Key Guarantees

- The skill never sees real secrets
- The skill cannot directly configure Envoy
- All outbound traffic is forced through the Network Boundary VM
- Every secret usage is auditable in the tamper-evident log

## Extensibility

The `network-access.yaml` format is designed to be extensible to support:
- Additional authentication schemes
- Non-HTTP protocols (WebSockets, gRPC, raw TCP)
- Custom signing logic (AWS SigV4, etc.)

New protocol handlers can be added to the Network Boundary VM over time without changing the core skill interface.
