import {
  EVENT,
  HarnessState,
  PIPELINE_STAGES,
  StageStatus,
} from '@/contracts';

export function defaultStages(): StageStatus[] {
  return PIPELINE_STAGES.map((name, i) => ({
    name,
    status: i === 0 ? 'in_progress' : 'pending',
  }));
}

export function applyHarnessEvent(
  state: HarnessState,
  event: Record<string, unknown>,
): HarnessState {
  if (!event?.type) return state;
  const next: HarnessState = structuredClone(state || { plan: null, tasks: [] });

  switch (event.type) {
    case EVENT.harnessPlanCreated:
      next.plan = {
        plan_id: String(event.plan_id ?? ''),
        channel_id: String(event.channel_id ?? ''),
        goal: String(event.goal ?? ''),
        stages: (event.stages as StageStatus[]) || defaultStages(),
        status: 'active',
      };
      break;
    case EVENT.harnessTaskAssigned:
      next.tasks = next.tasks || [];
      next.tasks.push({
        task_id: String(event.task_id ?? ''),
        plan_id: String(event.plan_id ?? ''),
        agent_persona: String(event.agent_persona ?? 'agent'),
        scope: String(event.scope ?? ''),
        current_stage: String(event.current_stage ?? 'Execute'),
        status: 'active',
        progress: 0,
      });
      break;
    case EVENT.harnessTaskProgress:
      next.tasks = (next.tasks || []).map((t) =>
        t.task_id === event.task_id
          ? {
              ...t,
              progress: Number(event.progress ?? t.progress),
              current_stage: String(event.current_stage ?? t.current_stage),
              summary: String(event.summary ?? t.summary ?? ''),
            }
          : t,
      );
      break;
    case EVENT.harnessStageTransition:
      if (next.plan?.stages) {
        next.plan.stages = next.plan.stages.map((s) =>
          s.name === event.stage
            ? { ...s, status: (event.status as StageStatus['status']) || s.status }
            : s,
        );
      }
      break;
    default:
      break;
  }
  return next;
}

export function slugStage(name: string): string {
  return String(name || '').toLowerCase().replace(/\s+/g, '-');
}