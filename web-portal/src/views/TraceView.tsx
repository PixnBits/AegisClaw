import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { AgentTrace } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';

export function TraceView() {
  const traceAgentId = usePortalStore((s) => s.traceAgentId);
  const [trace, setTrace] = useState<AgentTrace | null>(null);

  useEffect(() => {
    if (!traceAgentId) return;
    api.agentTrace(traceAgentId).then(setTrace).catch(() => setTrace(null));
  }, [traceAgentId]);

  const agentId = traceAgentId || 'agent';

  const handleAction = async (action: 'pause' | 'resume' | 'cancel') => {
    if (!confirm(`Confirm ${action} for ${agentId}?`)) return;
    await api.agentAction(agentId, action);
  };

  return (
    <section className="panel content-panel" data-testid="trace-panel" data-page="trace">
      <header>
        <p className="eyebrow">Deep Visibility</p>
        <h1 id="traceAgentTitle">Trace: {agentId}</h1>
        <p className="subtle">
          Agent <span id="currentAgentName">{agentId}</span> — Session <span id="currentTraceId">{trace?.session_id || agentId}</span>
        </p>
      </header>
      <div className="trace-actions" data-testid="trace-actions">
        <button type="button" className="secondary-button" data-testid="trace-pause" onClick={() => handleAction('pause')}>
          Pause
        </button>
        <button type="button" className="secondary-button" data-testid="trace-resume" onClick={() => handleAction('resume')}>
          Resume
        </button>
        <button type="button" className="danger-button" data-testid="trace-cancel" onClick={() => handleAction('cancel')}>
          Cancel
        </button>
      </div>
      <div className="trace-timeline" data-testid="trace-timeline">
        {!trace?.phases?.length ? (
          <p className="subtle" data-testid="trace-empty">
            No trace phases yet.
          </p>
        ) : (
          trace.phases.map((phase, i) => (
            <article key={i} className="list-card">
              <strong>{phase.phase}</strong>
              <p>{phase.summary}</p>
              {phase.tool && <code>{phase.tool}</code>}
            </article>
          ))
        )}
      </div>
    </section>
  );
}