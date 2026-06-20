import { useEffect, useMemo, useState } from 'react';
import { api } from '@/api/client';
import { NarrowTask } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';
import { formatPersonaLabel } from '@/lib/display';
import { PipelineStrip } from '@/components/CompactHarness/PipelineStrip';
import { EmptyState } from '@/components/ui/EmptyState';

type Props = {
  onOpenChannel: (id: string) => void;
};

type CanvasTask = NarrowTask & { channel_id: string };

export function CanvasView({ onOpenChannel }: Props) {
  const currentChannel = usePortalStore((s) => s.currentChannel);
  const harnessByChannel = usePortalStore((s) => s.harnessByChannel);
  const channelId = currentChannel?.id || 'main';
  const [canvas, setCanvas] = useState<Record<string, unknown> | null>(null);

  useEffect(() => {
    api.canvas(channelId).then(setCanvas).catch(() => setCanvas(null));
  }, [channelId]);

  const harness = harnessByChannel[channelId];

  const parallelTasks = useMemo(() => {
    const tasks: CanvasTask[] = [];
    const channels = Object.keys(harnessByChannel);
    for (const chId of channels.length ? channels : [channelId]) {
      const state = harnessByChannel[chId];
      for (const task of state?.tasks || []) {
        tasks.push({ ...task, channel_id: chId });
      }
    }
    if (tasks.length === 0 && canvas?.tasks) {
      for (const task of (canvas.tasks as NarrowTask[]) || []) {
        tasks.push({ ...task, channel_id: channelId });
      }
    }
    return tasks;
  }, [harnessByChannel, canvas, channelId]);

  return (
    <section className="panel content-panel content-panel--canvas" data-testid="canvas-panel" data-page="canvas" data-canvas-root>
      <header className="canvas-header" data-testid="canvas-header">
        <div>
          <p className="eyebrow">Inter-Agent Pipeline</p>
          <h1>Canvas</h1>
          <p className="subtle">
            Channel:{' '}
            <button type="button" className="link-button" data-testid="canvas-channel-link" onClick={() => onOpenChannel(channelId)}>
              {channelId}
            </button>
            {parallelTasks.length > 0 ? ` · ${parallelTasks.length} parallel task(s)` : ''}
          </p>
        </div>
      </header>
      <PipelineStrip stages={harness?.plan?.stages} />
      {parallelTasks.length === 0 ? (
        <EmptyState
          testId="canvas-empty-state"
          eyebrow="Pipeline"
          title="No parallel tasks yet"
          description="Submit a goal from Home or post in a channel to spin up the harness and see specialist work here."
          hint="Tasks appear live as the PM delegates work across personas."
        />
      ) : (
        <div className="canvas-stage-board card-grid" data-testid="canvas-agent-grid" data-canvas-grid>
          {parallelTasks.map((task) => (
            <article key={`${task.channel_id}-${task.task_id}`} className="list-card canvas-task-card">
              <div className="canvas-task-card__header">
                <strong>{formatPersonaLabel(task.agent_persona)}</strong>
                <span className="badge badge--active">{task.current_stage || 'Execute'}</span>
              </div>
              <p className="subtle">{task.scope}</p>
              <p className="subtle">Channel: {task.channel_id}</p>
              <div className="canvas-task-card__progress" aria-label={`Progress ${task.progress}%`}>
                <div className="canvas-task-card__progress-bar" style={{ width: `${Math.min(100, Math.max(0, task.progress || 0))}%` }} />
              </div>
              <span className="subtle">{task.progress ?? 0}% · {task.status}</span>
            </article>
          ))}
        </div>
      )}
      <article className="subpanel canvas-artifacts" data-testid="canvas-artifacts">
        <p className="eyebrow">Shared Artifacts</p>
        <p className="subtle">Research notes, diffs, and plans appear here as work progresses.</p>
      </article>
    </section>
  );
}
