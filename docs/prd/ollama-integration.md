# Ollama Integration & Model Strategy

## Goals
- Reliable tool calling, tool discovery (`tool.search`, `tool.list`), and instruction following.
- Support high-end hardware (128GB+ RAM) with best accuracy.
- Provide good quantized fallbacks.

## Security Considerations
Model choice is a high-risk supply-chain decision.

- Hidden triggers / sleeper agents: Specific phrases or conditions can bypass system prompts and safety alignments while appearing normal on benchmarks.
- Nation-state risks: Potential undisclosed capabilities in models from high-risk jurisdictions.
- Data poisoning: Persistent backdoors from small numbers of malicious examples.
- Post-release tampering on public platforms (Ollama, Hugging Face).

**Policy**: Prefer models with transparent provenance. Isolate all LLM calls through Network Boundary VM. Run local verification benchmarks including adversarial trigger tests.

## Recommended Models

**Default**: `gemma4:latest` (or gemma4:31b on high-end hardware)

**High-End (128GB+ RAM)**: `nemotron3-super:120b` or `gemma4:31b`

**Quantized Fallbacks**: Q4_K_M / Q5_K_M variants of the above (validated for minimal accuracy loss).

## Model Rubric
| Criterion | Weight | Minimum |
|-----------|--------|---------|
| Tool calling accuracy | High | ≥ 92% |
| Resistance to hidden triggers | High | Strong |
| Instruction following | High | Excellent |
| Supply-chain transparency | High | Verified |

## Benchmark
Maintain `docs/benchmarks/agent-tool-use/` with tool-use, instruction following, and adversarial trigger tests.