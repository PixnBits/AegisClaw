import { PortalView } from '@/contracts';

const ITEMS: { id: PortalView; label: string; testId: string }[] = [
  { id: 'channels', label: 'Channels', testId: 'bottom-nav-channels' },
  { id: 'dashboard', label: 'Dashboard', testId: 'bottom-nav-dashboard' },
  { id: 'court', label: 'Court', testId: 'bottom-nav-court' },
  { id: 'settings', label: 'More', testId: 'bottom-nav-more' },
];

type Props = {
  view: PortalView;
  onNavigate: (view: PortalView) => void;
};

export function BottomNav({ view, onNavigate }: Props) {
  return (
    <nav className="bottom-nav" aria-label="Mobile navigation" data-testid="bottom-nav">
      <div className="bottom-nav__items">
        {ITEMS.map((item) => (
          <button
            key={item.id}
            type="button"
            className={`bottom-nav__item${view === item.id || (item.id === 'settings' && ['home', 'agents', 'skills', 'audit', 'settings'].includes(view)) ? ' is-active' : ''}`}
            data-testid={item.testId}
            onClick={() => onNavigate(item.id)}
          >
            <span>{item.label}</span>
          </button>
        ))}
      </div>
    </nav>
  );
}