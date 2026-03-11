# PlannerSkill Prompt

You are generating **PlannerSkill** — a task decomposition and planning skill for complex goals in SeedClaw.

## Purpose
- Break high-level user goals into ordered/parallel subtasks
- Create executable plans (sequence or DAG of steps)
- Re-plan on failure/reflection feedback
- Route subtasks to appropriate skills via message hub

## Strict Rules
1. Use stdlib only; represent plans as JSON structs (tasks with deps, assignees)
2. Input via stdin JSON: goal description, available skills (from registry or registration messages)
3. Output plan: list of steps with "task", "description", "depends_on" [], "assigned_to" skill name or "hub"
4. Support sequential + parallel (no deps = parallel)
5. Include contingency: "on_failure" actions (retry, reflect, escalate)
6. Use ReAct-style thinking: Thought → Subtasks → Assignment
7. Register on startup; forward plans via stdout messages

## Output JSON Format

```json
{
  "skill_name": "PlannerSkill",
  "description": "Decomposes goals into subtasks, creates plans, routes to skills",
  "prompt_template": "You are PlannerSkill. Break goals into steps, assign to skills, re-plan on feedback.",
  "go_package": "main",
  "source_code": "... complete Go code ...",
  "binary_name": "planner",
  "build_flags": ["-trimpath", "-ldflags=-s -w"],
  "tests_included": true,
  "test_command": "go test -v ./..."
}
```

Generate implementation that parses goal, outputs structured plan JSON via messages + tests (decompose simple/complex goals).

Example high-value task: "After generating GitSkill, retrieve all pre-git generated skills from MemoryReflectionSkill and commit them via GitSkill"
