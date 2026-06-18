import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { usePortalStore } from '@/store/portalStore';
import { PipelineStrip } from '@/components/CompactHarness/PipelineStrip';

type Props = {
  onOpenChannel: (id: string) => void;
};

export function CanvasView({ onOpenChannel }: Props) {
  const currentChannel = usePortalStore((s) => s.currentChannel);
  const harnessByChannel = usePortalStore((s) => s.harnessByChannel);
  const channelId = currentChannel?.id || 'main';
  const [canvas, setCanvas] = useState<Record<string, unknown> | null>(null);

  useEffect(() => {
    api.canvas(channelId).then(setCanvas).catch(() => setCanvas(null));
  }, [channelId]);

  const harness = harnessByChannel[channelId];
  const agents = (canvas?.agents as Array<Record<string, unknown>>) || harness?.tasks || [];

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
          </p>
        </div>
      </header>
      <PipelineStrip stages={harness?.plan?.stages} />
      <div className="canvas-stage-board card-grid" data-testid="canvas-agent-grid" data-canvas-grid>
        {agents.map((agent, i) => (
          <article key={String(agent.task_id || agent.id || i)} className="list-card">
            <strong>{String(agent.agent_persona || agent.persona || agent.name || 'agent')}</strong>
            <p className="subtle">{String(agent.scope || agent.task || '')}</p>
            <span className="badge badge--active">{String(agent.current_stage || agent.stage || 'Execute')}</span>
          </article>
        ))}
      </div>
      <article className="subpanel canvas-artifacts" data-testid="canvas-artifacts">
        <p className="eyebrow">Shared Artifacts</p>
        <p className="subtle">Research notes, diffs, and plans appear here as work progresses.</p>
      </article>
    </section>
  );
}