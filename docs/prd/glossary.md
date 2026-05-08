# Glossary

## Core Concepts

- **AegisClaw** — The overall local-first, paranoid-by-design agent platform.
- **Governance Court** — The five-persona review board (Coder, Tester, CISO, Security Architect, User Advocate) that must approve every change.
- **MicroVM** — A Firecracker-based virtual machine. Every untrusted component runs in its own isolated microVM.
- **Host Daemon** — The only code that runs directly on the host OS. Kept extremely minimal by design.

## Key Components

- **AegisHub** — The privileged but minimal router that mediates all communication between microVMs and enforces ACLs.
- **Network Boundary VM** — The only microVM allowed to handle secrets. Runs Envoy to proxy all outbound network traffic.
- **Court Scribe** — A lightweight isolated component that observes conversations and produces structured summaries for the Governance Court.
- **Store VM** — Dedicated microVM that owns all persistent storage and the tamper-evident audit log.
- **LLM Proxy VM** — Component responsible for sanitizing prompts and ensuring secrets never reach the LLM.

## Important Terms

- **Skill** — A single capability or tool that an agent can use. Must be approved by the Governance Court before use.
- **Network Access Declaration** (`network-access.yaml`) — A strict, declarative file that defines exactly what network access a skill is allowed to have.
- **Autonomy Level** — A trust tier for agents (Level 0 = Passive, Level 1 = Proactive, Level 2 = Independent).
- **Change Proposal** — A formal request to add, modify, or remove any code, skill, or configuration in the system.
- **Tamper-Evident Audit Log** — An append-only log protected by a Merkle tree. Every action is cryptographically signed.

## Architecture Principles

- **Paranoid-by-Design** — The belief that all external input and generated code is potentially malicious.
- **Every Boundary is a Security Boundary** — The guiding rule that no two components should directly trust each other.

