# Ollama Integration Specification

## Overview
Agent Runtime connects to Ollama via HTTP API. All traffic must route through Network Boundary VM for auditing and control.

## Security Considerations
- Hidden triggers and sleeper agents in models
- Nation-state supply-chain risks
- Data poisoning and post-release tampering

**All LLM calls must go through Network Boundary.**

## Recommended Models
Default: qwen3-coder:30b
High-end: nemotron3-super:120b or gemma4:31b

## Configuration
Set in ~/.aegis/config.yaml (`default_model`)
Default connection: http://localhost:11434 (routed through Network Boundary).