import { HarnessState, NarrowTask } from '@/contracts';
import { formatPersonaLabel } from '@/lib/display';
import { useIsMobile } from '@/hooks/useMediaQuery';
import './AgentActivitySummary.css';

type Props = {
  harness?: HarnessState | null;
  tokenUsage?: number;
  onDrillDown?: () => void;
  compact?: boolean;
  /** Fallback personas when harness has no active tasks (e.g. from channel roster) */
  idlePersonas?: string[];
};

function activeTasks(tasks: NarrowTask[]): NarrowTask[] {
  return (tasks || []).filter((t) => t.status === 'active' || (t.progress ?? 0) < 100);
}

function stageProgress(plan: HarnessState['plan']): number {
  if (!plan?.stages?.length) return 0;
  const completed = plan.stages.filter((s) => s.status === 'completed').length;
  return Math.round((completed / plan.stages.length) * 100);
}

export function AgentActivitySummary({
  harness,
  tokenUsage,
  onDrillDown,
  compact,
  idlePersonas = [],
}: Props) {
  const isMobile = useIsMobile();
  const tasks = activeTasks(harness?.tasks || []);
  const progress = stageProgress(harness?.plan ?? null);
  const hasGoal = Boolean(harness?.plan?.goal);
  const hasWork = tasks.length > 0;

  if (!hasWork && !hasGoal && idlePersonas.length === 0) {
    return (
      <div
        className={`activity-summary activity-summary--empty${compact ? ' activity-summary--compact' : ''}`}
        data-testid="agent-activity-summary"
        aria-live="polite"
        role="region"
        aria-label="Agent activity summary"
      >
        <span className="activity-summary__pulse" aria-hidden="true" />
        <div className="activity-summary__empty-text">
          <span className="activity-summary__empty-label">Ready when you are</span>
          <span className="subtle">Submit a goal and the PM will spin up narrow tasks for specialists.</span>
        </div>
      </div>
    );
  }

  const displayChips = hasWork
    ? tasks.slice(0, compact ? 6 : 4).map((task) => ({
        key: task.task_id,
        label: formatPersonaLabel(task.agent_persona),
        stage: task.current_stage,
        detail: `${task.progress}%`,
      }))
    : idlePersonas.slice(0, compact ? 6 : 4).map((p) => ({
        key: p,
        label: formatPersonaLabel(p),
        stage: 'Idle',
        detail: null as string | null,
      }));

  return (
    <div
      className={`activity-summary${compact ? ' activity-summary--compact' : ''}${hasWork ? ' activity-summary--live' : ''}`}
      data-testid="agent-activity-summary"
      aria-live="polite"
      role="region"
      aria-label="Agent activity summary"
    >
      <div className="activity-summary__header">
        <p className="eyebrow">Activity</p>
        {onDrillDown && (
          <button type="button" className="link-button activity-summary__drill" onClick={onDrillDown}>
            View details
          </button>
        )}
      </div>
      <div
        className="activity-summary__chips"
        tabIndex={isMobile || compact ? 0 : undefined}
        role={isMobile || compact ? 'region' : undefined}
        aria-label={isMobile || compact ? 'Active agents' : undefined}
      >
        {displayChips.map((chip) => (
          <button
            key={chip.key}
            type="button"
            className="activity-summary__chip"
            data-testid={`activity-chip-${chip.key}`}
            onClick={onDrillDown}
          >
            <span className="activity-summary__persona">{chip.label}</span>
            <span className={`badge ${chip.stage === 'Idle' ? 'badge--pending' : 'badge--active'}`}>{chip.stage}</span>
            {chip.detail && <span className="activity-summary__progress">{chip.detail}</span>}
          </button>
        ))}
        {(hasWork ? tasks.length : idlePersonas.length) > (compact ? 6 : 4) && (
          <span className="activity-summary__more subtle">
            +{(hasWork ? tasks.length : idlePersonas.length) - (compact ? 6 : 4)} more
          </span>
        )}
      </div>
      <div className="activity-summary__progress-row">
        <div
          className="activity-summary__progress-bar"
          role="progressbar"
          aria-valuenow={progress}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label={`Pipeline ${progress}% complete`}
        >
          <div className="activity-summary__progress-fill" style={{ width: `${progress}%` }} />
        </div>
        <div className="activity-summary__meta">
          <span data-testid="activity-stage-progress">Pipeline {progress}%</span>
          {tokenUsage != null && (
            <span data-testid="activity-token-usage">{tokenUsage.toLocaleString()} tokens</span>
          )}
        </div>
      </div>
      {hasGoal && !compact && (
        <p className="activity-summary__goal subtle">{harness!.plan!.goal}</p>
      )}
    </div>
  );
}