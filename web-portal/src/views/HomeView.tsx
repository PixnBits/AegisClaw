import { FormEvent, useState } from 'react';
import { usePortalStore } from '@/store/portalStore';
import { AgentActivitySummary } from '@/components/AgentActivitySummary/AgentActivitySummary';
import { useIsMobile } from '@/hooks/useMediaQuery';

type Props = {
  onOpenChannel: (channelId: string) => void;
  onOpenCanvas: () => void;
};

export function HomeView({ onOpenChannel, onOpenCanvas }: Props) {
  const isMobile = useIsMobile();
  const dashboard = usePortalStore((s) => s.dashboard);
  const planPreview = usePortalStore((s) => s.planPreview);
  const harnessByChannel = usePortalStore((s) => s.harnessByChannel);
  const overviewStats = usePortalStore((s) => s.overviewStats);
  const submitGoal = usePortalStore((s) => s.submitGoal);
  const clearPlanPreview = usePortalStore((s) => s.clearPlanPreview);
  const [goal, setGoal] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const mainHarness = harnessByChannel.main;
  const tokenUsage = overviewStats?.token_usage?.total;

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    const text = goal.trim();
    if (!text) return;
    setSubmitting(true);
    try {
      await submitGoal(text);
      setGoal('');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <section className="panel content-panel content-panel--home" data-testid="home-panel" data-page="home">
      <form className="command-bar command-bar--hero" data-testid="command-bar" onSubmit={handleSubmit} noValidate>
        <p className="eyebrow">Command Center</p>
        <label htmlFor="commandBarInput" className="command-bar__label">
          What do you want to accomplish?
        </label>
        <textarea
          id="commandBarInput"
          rows={4}
          maxLength={4000}
          placeholder="Describe your goal in natural language…"
          data-testid="command-bar-input"
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
        />
        <p className="subtle">The PM will decompose your goal into narrow tasks for specialists with Court review.</p>
        <button type="submit" className="primary-button" data-testid="command-bar-submit" disabled={submitting}>
          {submitting ? 'Starting…' : 'Start Plan Preview'}
        </button>
      </form>

      {planPreview && (
        <article className="subpanel plan-preview" data-testid="plan-preview">
          <p className="eyebrow">Plan Preview</p>
          <p data-preview-goal>{planPreview.goal}</p>
          <p className="subtle">
            Channel: <span data-preview-channel>{planPreview.channel_id}</span>
          </p>
          <div className="button-stack">
            <button type="button" className="secondary-button" onClick={onOpenCanvas}>
              View Canvas
            </button>
            <button
              type="button"
              className="primary-button"
              data-testid="plan-preview-open"
              onClick={() => {
                onOpenChannel(planPreview.channel_id);
                clearPlanPreview();
              }}
            >
              Open Channel
            </button>
          </div>
        </article>
      )}

      {!isMobile && (
        <AgentActivitySummary
          harness={mainHarness}
          tokenUsage={tokenUsage}
          onDrillDown={() => onOpenChannel('main')}
        />
      )}

      <div className="live-pulse live-pulse--subtle" data-testid="live-pulse">
        <span>
          <strong>{overviewStats?.active_agents?.total ?? dashboard?.quick_stats?.active_agents ?? 0}</strong> active agents
        </span>
        <span>
          <strong>{overviewStats?.pending_proposals ?? dashboard?.quick_stats?.pending_proposals ?? 0}</strong> pending proposals
        </span>
      </div>

      <article className="subpanel home-recent" data-testid="home-recent-activity">
        <p className="eyebrow">Recent Activity</p>
        <ul className="list-stack" id="homeRecentList">
          {(dashboard?.active_work || []).slice(0, 3).map((w) => (
            <li key={w.id} className="list-card">
              <strong>{w.scope || w.persona}</strong>
              <small className="subtle">
                {w.stage} • {w.channel_id}
              </small>
            </li>
          ))}
        </ul>
      </article>
    </section>
  );
}