import { HarnessState, NarrowTask } from '@/contracts';
import './AgentActivitySummary.css';

type Props = {
  harness?: HarnessState | null;
  tokenUsage?: number;
  onDrillDown?: () => void;
  compact?: boolean;
};

function activeTasks(tasks: NarrowTask[]): NarrowTask[] {
  return (tasks || []).filter((t) => t.status === 'active' || t.progress < 100);
}

function stageProgress(stages: HarnessState['plan']): number {
  if (!stages?.stages?.length) return 0;
  const completed = stages.stages.filter((s) => s.status === 'completed').length;
  return Math.round((completed / stages.stages.length) * 100);
}

export function AgentActivitySummary({ harness, tokenUsage, onDrillDown, compact }: Props) {
  const tasks = activeTasks(harness?.tasks || []);
  const progress = stageProgress(harness?.plan ?? null);

  if (!tasks.length && !harness?.plan?.goal) {
    return (
      <div
        className={`activity-summary activity-summary--empty${compact ? ' activity-summary--compact' : ''}`}
        data-testid="agent-activity-summary"
        aria-live="polite"
      >
        <span className="subtle">No active work</span>
      </div>
    );
  }

  return (
    <div
      className={`activity-summary${compact ? ' activity-summary--compact' : ''}`}
      data-testid="agent-activity-summary"
      aria-live="polite"
      role="region"
      aria-label="Agent activity summary"
    >
      <div className="activity-summary__chips">
        {tasks.slice(0, 4).map((task) => (
          <button
            key={task.task_id}
            type="button"
            className="activity-summary__chip"
            data-testid={`activity-chip-${task.task_id}`}
            onClick={onDrillDown}
          >
            <span className="activity-summary__persona">{task.agent_persona}</span>
            <span className="badge badge--active">{task.current_stage}</span>
            <span className="activity-summary__progress">{task.progress}%</span>
          </button>
        ))}
        {tasks.length > 4 && (
          <span className="activity-summary__more subtle">+{tasks.length - 4} more</span>
        )}
      </div>
      <div className="activity-summary__meta">
        <span data-testid="activity-stage-progress">Stage {progress}%</span>
        {tokenUsage != null && (
          <span data-testid="activity-token-usage">{tokenUsage.toLocaleString()} tokens</span>
        )}
      </div>
    </div>
  );
}