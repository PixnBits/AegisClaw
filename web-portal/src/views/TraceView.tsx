import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { MarkdownContent } from '@/components/ui/MarkdownContent';
import { VirtualList } from '@/components/ui/VirtualList';
import { AgentTrace } from '@/contracts';
import { agentActionConfirmCopy, type AgentControlAction } from '@/lib/confirmCopy';
import { usePortalStore } from '@/store/portalStore';

export function TraceView() {
  const traceAgentId = usePortalStore((s) => s.traceAgentId);
  const safeMode = usePortalStore((s) => s.dashboard?.safe_mode);
  const [trace, setTrace] = useState<AgentTrace | null>(null);
  const [pendingAction, setPendingAction] = useState<AgentControlAction | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    if (!traceAgentId) return;
    api.agentTrace(traceAgentId).then(setTrace).catch(() => setTrace(null));
  }, [traceAgentId]);

  const agentId = traceAgentId || 'agent';
  const controlsLocked = safeMode === true;

  const handleConfirm = async () => {
    if (!pendingAction) return;
    setActionError(null);
    try {
      await api.agentAction(agentId, pendingAction);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Agent action failed');
    } finally {
      setPendingAction(null);
    }
  };

  const dialogCopy = pendingAction ? agentActionConfirmCopy(pendingAction, agentId) : null;

  return (
    <section className="panel content-panel" data-testid="trace-panel" data-page="trace">
      <header>
        <p className="eyebrow">Deep Visibility</p>
        <h1 id="traceAgentTitle">Trace: {agentId}</h1>
        <p className="subtle">
          Agent <span id="currentAgentName">{agentId}</span> — Session{' '}
          <span id="currentTraceId">{trace?.session_id || agentId}</span>
        </p>
        {controlsLocked && (
          <p className="subtle" data-testid="trace-safe-mode-notice">
            Safe Mode is ON — agent controls are disabled.
          </p>
        )}
      </header>
      <div className="trace-actions" data-testid="trace-actions">
        <button
          type="button"
          className="secondary-button"
          data-testid="trace-pause"
          disabled={controlsLocked}
          onClick={() => setPendingAction('pause')}
        >
          Pause
        </button>
        <button
          type="button"
          className="secondary-button"
          data-testid="trace-resume"
          disabled={controlsLocked}
          onClick={() => setPendingAction('resume')}
        >
          Resume
        </button>
        <button
          type="button"
          className="danger-button"
          data-testid="trace-cancel"
          disabled={controlsLocked}
          onClick={() => setPendingAction('cancel')}
        >
          Cancel
        </button>
      </div>
      {actionError && (
        <p className="subtle" role="alert" data-testid="trace-action-error">
          {actionError}
        </p>
      )}
      <div className="trace-timeline" data-testid="trace-timeline">
        {!trace?.phases?.length ? (
          <p className="subtle" data-testid="trace-empty">
            No trace phases yet.
          </p>
        ) : (
          <VirtualList
            items={trace.phases}
            getKey={(phase, i) => `${phase.phase}-${phase.ts || i}`}
            testId="trace-timeline-virtual"
            estimateItemHeight={120}
            renderItem={(phase) => (
              <article className="list-card">
                <strong>{phase.phase}</strong>
                <div className="trace-phase-summary">
                  <MarkdownContent content={phase.summary || ''} context="trace" />
                </div>
                {phase.tool && <code>{phase.tool}</code>}
              </article>
            )}
          />
        )}
      </div>

      <ConfirmDialog
        open={pendingAction !== null}
        title={dialogCopy?.title ?? 'Confirm'}
        message={dialogCopy?.message ?? ''}
        confirmLabel={pendingAction === 'cancel' ? 'Cancel run' : 'Continue'}
        variant={dialogCopy?.variant ?? 'primary'}
        onConfirm={() => void handleConfirm()}
        onCancel={() => setPendingAction(null)}
        testId="trace-confirm-dialog"
      />
    </section>
  );
}
