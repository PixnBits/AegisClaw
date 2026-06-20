import { describe, expect, it } from 'vitest';
import { applyHarnessEvent, defaultStages } from './harness';
import { EVENT } from '@/contracts';

describe('harness event application', () => {
  it('creates plan on harness.plan.created', () => {
    const state = applyHarnessEvent({ plan: null, tasks: [] }, {
      type: EVENT.harnessPlanCreated,
      plan_id: 'plan_main',
      channel_id: 'main',
      goal: 'Research Zig',
      stages: defaultStages(),
    });
    expect(state.plan?.goal).toBe('Research Zig');
    expect(state.plan?.stages).toHaveLength(6);
  });

  it('assigns narrow task', () => {
    const state = applyHarnessEvent({ plan: null, tasks: [] }, {
      type: EVENT.harnessTaskAssigned,
      task_id: 'task_1',
      plan_id: 'plan_main',
      agent_persona: 'researcher',
      scope: 'Compare Zig vs Rust',
      current_stage: 'Execute',
    });
    expect(state.tasks).toHaveLength(1);
    expect(state.tasks[0].agent_persona).toBe('researcher');
  });

  it('updates task progress', () => {
    const initial = applyHarnessEvent({ plan: null, tasks: [] }, {
      type: EVENT.harnessTaskAssigned,
      task_id: 'task_1',
      plan_id: 'plan_main',
      agent_persona: 'researcher',
      scope: 'Test',
      current_stage: 'Execute',
    });
    const updated = applyHarnessEvent(initial, {
      type: EVENT.harnessTaskProgress,
      task_id: 'task_1',
      progress: 75,
      current_stage: 'Execute',
      summary: 'Found 12 papers',
    });
    expect(updated.tasks[0].progress).toBe(75);
    expect(updated.tasks[0].summary).toBe('Found 12 papers');
  });
});