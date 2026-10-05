# Arm B — ASD-STE100

Prompt-only Simplified Technical English. No few-shots.

`internal/ste.Instruction` is the shared writing constraint. It is prepended to Project Manager prompts (`getPMPrompt`, `getPMPlanPrompt`, `getPMChannelPrompt`) and to the agent `customInstructionsPrefix`, including when the workspace is empty. Court is included: `workspaceCustomPrefix` always carries the instruction (so `getPersonaPrompt` does), and `channelDecisionPreamble` appends it so every SPEAK/PASS persona prompt includes it.

Comparison base is `exp/base-metrics`. This arm does not change routing, tools, or examples.
