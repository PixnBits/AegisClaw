import { HarnessState } from '@/contracts';
import { PipelineStrip } from './PipelineStrip';
import './CompactHarness.css';

type Props = {
  state: HarnessState | null | undefined;
  onOpenCanvas?: () => void;
};

export function CompactHarness({ state, onOpenCanvas }: Props) {
  const goal = state?.plan?.goal || 'No active plan — use the command bar to start.';

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
          <button type="button" className="secondary-button" onClick={onOpenCanvas} data-testid="open-canvas-from-harness">
            Open Canvas
          </button>
        )}
      </div>
      <PipelineStrip stages={state?.plan?.stages} />
      <div className="harness-tasks" data-testid="harness-tasks">
        {!state?.tasks?.length ? (
          <p className="subtle">No narrow tasks yet — submit a goal to start decomposition.</p>
        ) : (
          state.tasks.map((task) => (
            <article key={task.task_id} className="harness-task-card" data-testid={`task-${task.task_id}`}>
              <div className="harness-task-card__header">
                <strong>{task.agent_persona}</strong>
                <span className={`badge badge--${task.status || 'pending'}`}>{task.current_stage}</span>
              </div>
              <p className="harness-task-card__scope">{task.scope}</p>
              <div className="harness-task-card__progress" aria-label={`Progress ${task.progress}%`}>
                <div className="harness-task-card__bar" style={{ width: `${Math.min(100, task.progress)}%` }} />
              </div>
            </article>
          ))
        )}
      </div>
    </section>
  );
}