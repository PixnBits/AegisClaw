import { PortalView } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';

const NAV_ITEMS: { id: PortalView; label: string; testId: string; mobileHidden?: boolean; overflow?: boolean }[] = [
  { id: 'home', label: 'Home', testId: 'nav-home' },
  { id: 'channels', label: 'Channels', testId: 'nav-channels' },
  { id: 'dashboard', label: 'Dashboard', testId: 'nav-dashboard' },
  { id: 'canvas', label: 'Canvas', testId: 'nav-canvas', mobileHidden: true },
  { id: 'court', label: 'Court', testId: 'nav-court' },
  { id: 'agents', label: 'Agents', testId: 'nav-agents', mobileHidden: true },
  { id: 'skills', label: 'Skills', testId: 'nav-skills', mobileHidden: true },
  { id: 'audit', label: 'Audit', testId: 'nav-audit', mobileHidden: true, overflow: true },
  { id: 'settings', label: 'Settings', testId: 'nav-settings', mobileHidden: true },
];

type Props = {
  onNavigate: (view: PortalView) => void;
};

export function TopBar({ onNavigate }: Props) {
  const view = usePortalStore((s) => s.view);
  const dashboard = usePortalStore((s) => s.dashboard);
  const connectionMode = usePortalStore((s) => s.connectionMode);

  const connLabel =
    connectionMode === 'stomp' ? 'Conn STOMP' : connectionMode === 'sse-fallback' ? 'Conn SSE' : 'Conn …';

  return (
    <header className="topbar" data-testid="topbar">
      <button
        type="button"
        className="brand-block brand-block--home"
        data-testid="brand-home"
        aria-label="Go to Home"
        onClick={() => onNavigate('home')}
      >
        <span className="brand-mark" aria-hidden="true">
          🛡️
        </span>
        <div>
          <p className="eyebrow">AegisClaw</p>
          <strong>Secure Command Center</strong>
        </div>
      </button>

      <nav className="primary-nav" aria-label="Primary">
        {NAV_ITEMS.map((item) => (
          <button
            key={item.id}
            type="button"
            className={`nav-button${view === item.id ? ' is-active' : ''}${item.mobileHidden ? ' nav-button--mobile-hidden' : ''}${item.overflow ? ' nav-button--overflow' : ''}`}
            data-testid={item.testId}
            onClick={() => onNavigate(item.id)}
          >
            {item.label}
          </button>
        ))}
      </nav>

      <div className="topbar-meta">
        <div className="status-chip status-chip--compact" data-testid="system-status-chip">
          <span className="status-dot status-dot--success" aria-hidden="true" />
          <span>{dashboard?.system_status || 'Running'}</span>
          <span className="status-divider">|</span>
          <span>{dashboard?.runtime || 'Firecracker'}</span>
        </div>
        <div className="status-chip status-chip--compact" data-testid="connection-status-chip">
          <span className="status-dot status-dot--success" aria-hidden="true" />
          <span data-testid="connection-status-label">{connLabel}</span>
        </div>
        <button type="button" className="avatar-button" data-testid="avatar-button" title="Notifications">
          {dashboard?.notifications ?? 0}
        </button>
      </div>
    </header>
  );
}