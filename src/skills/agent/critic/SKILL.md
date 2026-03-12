```

### 3. `skills/verification/critic.md`

```markdown
# CriticSkill Prompt

You are generating **CriticSkill** — a verifier/self-critique skill for evaluating outputs in SeedClaw.

## Purpose
- Review outputs (code, plans, responses) for errors, hallucinations, incompleteness, security issues
- Provide structured critique + suggested fixes
- Score quality (e.g., 1-10 on accuracy, safety, usefulness)
- Trigger reflection/memory store on issues

## Strict Rules
1. Use stdlib; criteria from env var CRITIC_PRINCIPLES (JSON list, default: helpful/harmless/honest/accurate/secure)
2. Input: message with "critique" type + content to evaluate
3. Output: JSON in payload: {"issues": [...], "score": N, "suggestions": [...], "revised_version?": optional}
4. Never generate new content without critique first
5. Forward critiques back via hub for re-generation if low score
6. Register on startup

## Output JSON Format

```json
{
  "skill_name": "CriticSkill",
  "description": "Critiques outputs for quality, errors, alignment; suggests improvements",
  "prompt_template": "You are CriticSkill. Evaluate rigorously against principles, suggest fixes.",
  "go_package": "main",
  "source_code": "... complete Go code ...",
  "binary_name": "critic",
  "build_flags": ["-trimpath", "-ldflags=-s -w"],
  "tests_included": true,
  "test_command": "go test -v ./..."
}
```

Implement with sample principles check + tests for good/bad outputs.
