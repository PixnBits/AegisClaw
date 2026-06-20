import { useEffect } from 'react';
import { usePortalStore } from '@/store/portalStore';
import { AgentActivitySummary } from '@/components/AgentActivitySummary/AgentActivitySummary';
import { api } from '@/api/client';
import { MonitoringStats } from '@/contracts';
import { formatMemoryUsage } from '@/lib/display';

type Props = {
  onOpenCanvas: () => void;
  onOpenTrace: (agentId: string) => void;
  onOpenCourt: (proposalId: string) => void;
};

export function DashboardView({ onOpenCanvas, onOpenTrace, onOpenCourt }: Props) {
  const dashboard = usePortalStore((s) => s.dashboard);
  const harnessByChannel = usePortalStore((s) => s.harnessByChannel);
  const dashboardFilter = usePortalStore((s) => s.dashboardFilter);
  const setDashboardFilter = usePortalStore((s) => s.setDashboardFilter);
  const overviewStats = usePortalStore((s) => s.overviewStats);
  const monitoringStats = usePortalStore((s) => s.monitoringStats);

  useEffect(() => {
    api.monitoring().then((data) => {
      usePortalStore.setState({
        monitoringStats: {
          type: 'monitoring.stats',
          timestamp: new Date().toISOString(),
          stats: data.stats as MonitoringStats['stats'],
          agents: data.agents,
        },
      });
    }).catch(() => {});
  }, []);

  const liveStats = monitoringStats?.stats;

  const activeWork = dashboard?.active_work || [];
  const filtered =
    dashboardFilter === 'all'
      ? activeWork
      : activeWork.filter((w) => w.stage?.toLowerCase().includes(dashboardFilter) || w.persona === dashboardFilter);

  const mainHarness = harnessByChannel.main;

  return (
    <section className="panel content-panel" data-testid="dashboard-panel" data-page="dashboard">
      <header>
        <p className="eyebrow">Monitoring</p>
        <h1>Dashboard</h1>
      </header>

      <div className="filter-bar" data-testid="dashboard-filters">
        <label>
          Filter
          <select id="dashboardFilter" data-testid="dashboard-filter" value={dashboardFilter} onChange={(e) => setDashboardFilter(e.target.value)}>
            <option value="all">All</option>
            <option value="execute">By Stage: Execute</option>
            <option value="court">Court Review</option>
          </select>
        </label>
        <button type="button" className="secondary-button" data-testid="open-canvas-button" onClick={onOpenCanvas}>
          Open Canvas
        </button>
      </div>

      <AgentActivitySummary
        harness={mainHarness}
        tokenUsage={overviewStats?.token_usage?.total}
        onDrillDown={onOpenCanvas}
      />

      <div className="stats-grid" data-testid="dashboard-stats">
        <div className="stat-card">
          <span className="eyebrow">Active Agents</span>
          <strong id="statActiveAgents">
            {overviewStats?.active_agents?.total ?? dashboard?.quick_stats?.active_agents ?? 0}
          </strong>
        </div>
        <div className="stat-card">
          <span className="eyebrow">Background Tasks</span>
          <strong id="statBackgroundTasks">{dashboard?.quick_stats?.background_tasks ?? 0}</strong>
        </div>
        <div className="stat-card">
          <span className="eyebrow">Pending Proposals</span>
          <strong id="statPendingProposals">
            {overviewStats?.pending_proposals ?? dashboard?.quick_stats?.pending_proposals ?? 0}
          </strong>
        </div>
        <div className="stat-card">
          <span className="eyebrow">Channels</span>
          <strong id="statChannels">{dashboard?.channel_count ?? 0}</strong>
        </div>
      </div>

      <details className="dashboard-disclosure" open data-testid="dashboard-system-health">
        <summary>System Health</summary>
        <div className="stats-grid">
          <div className="stat-card">
            <span className="eyebrow">Running VMs</span>
            <strong id="statRunningVMs">{String(liveStats?.running_vms ?? 0)}</strong>
          </div>
          <div className="stat-card">
            <span className="eyebrow">CPU</span>
            <strong id="statCPUUsage">{String(liveStats?.cpu_usage ?? '—')}</strong>
          </div>
          <div className="stat-card">
            <span className="eyebrow">Memory</span>
            <strong id="statMemoryUsage">{formatMemoryUsage(liveStats?.memory_usage)}</strong>
          </div>
        </div>
      </details>

      <article className="subpanel subpanel--hero" data-testid="active-work-panel">
        <p className="eyebrow">Active Work</p>
        <div className="card-grid" data-testid="active-work-list">
          {filtered.map((item) => (
            <article key={item.id} className="list-card">
              <strong>{item.scope || item.persona}</strong>
              <p className="subtle">
                {item.stage} • {item.channel_id}
              </p>
              <div className="button-stack">
                <button type="button" className="secondary-button" onClick={() => onOpenTrace(item.id)}>
                  Trace
                </button>
                <button type="button" className="secondary-button" onClick={onOpenCanvas}>
                  Canvas
                </button>
                {item.proposal_id && (
                  <button type="button" className="secondary-button" onClick={() => onOpenCourt(item.proposal_id!)}>
                    Court
                  </button>
                )}
              </div>
            </article>
          ))}
        </div>
      </article>

      <div className="safe-mode-bar" data-testid="safe-mode-banner">
        <span>
          Safe Mode: <strong id="safeModeLabel">{dashboard?.safe_mode ? 'ON' : 'OFF'}</strong>
        </span>
        <button type="button" className="danger-button" data-testid="safe-mode-toggle">
          Enable Safe Mode
        </button>
      </div>
    </section>
  );
}
