import { HarnessState } from '@/contracts';
import { formatPersonaLabel } from '@/lib/display';
import { useIsMobile } from '@/hooks/useMediaQuery';
import { PipelineStrip } from './PipelineStrip';
import './CompactHarness.css';

type Props = {
  state: HarnessState | null | undefined;
  onOpenCanvas?: () => void;
  /** Hide task cards when Activity Summary already surfaces them (mobile density) */
  compactTasks?: boolean;
};

export function CompactHarness({ state, onOpenCanvas, compactTasks }: Props) {
  const isMobile = useIsMobile();
  const goal = state?.plan?.goal || 'No active plan — use the command bar to start.';
  const hasTasks = Boolean(state?.tasks?.length);
  const scrollTasks = !compactTasks && hasTasks && isMobile;

  return (
    <section className="harness-overview" data-testid="harness-overview">
      <div className="harness-overview__header">
        <div>
          <p className="eyebrow">Harness / Pipeline</p>
          <p className="harness-overview__goal" data-harness-goal>
            {goal}
          </p>
        </div>
        {onOpenCanvas && (
          <button type="button" className="secondary-button secondary-button--small" onClick={onOpenCanvas} data-testid="open-canvas-from-harness">
            Canvas
          </button>
        )}
      </div>
      <PipelineStrip stages={state?.plan?.stages} />
      <div
        className={`harness-tasks${compactTasks ? ' harness-tasks--compact' : ''}${scrollTasks ? ' harness-tasks--scroll' : ''}`}
        data-testid="harness-tasks"
        tabIndex={scrollTasks ? 0 : undefined}
        role={scrollTasks ? 'region' : undefined}
        aria-label={scrollTasks ? 'Narrow tasks' : undefined}
      >
        {compactTasks ? (
          <p className="subtle harness-tasks__hint">
            {state?.tasks?.length
              ? `${state.tasks.length} narrow task(s) — open Canvas for full pipeline view`
              : 'No narrow tasks yet — submit a goal to start decomposition.'}
          </p>
        ) : !state?.tasks?.length ? (
          <p className="subtle">No narrow tasks yet — submit a goal to start decomposition.</p>
        ) : (
          state.tasks.map((task) => (
            <article key={task.task_id} className="harness-task-card" data-testid={`task-${task.task_id}`}>
              <div className="harness-task-card__header">
                <strong>{formatPersonaLabel(task.agent_persona)}</strong>
                <span className={`badge badge--${task.status || 'pending'}`}>{task.current_stage}</span>
              </div>
              <p className="harness-task-card__scope">{task.scope}</p>
              <div
                className="harness-task-card__progress"
                role="progressbar"
                aria-valuenow={task.progress}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-label={`Progress ${task.progress}%`}
              >
                <div className="harness-task-card__bar" style={{ width: `${Math.min(100, task.progress)}%` }} />
              </div>
            </article>
          ))
        )}
      </div>
    </section>
  );
}