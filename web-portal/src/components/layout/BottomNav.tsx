import { PortalView } from '@/contracts';

const ITEMS: { id: PortalView | 'more'; label: string; testId: string }[] = [
  { id: 'channels', label: 'Channels', testId: 'bottom-nav-channels' },
  { id: 'dashboard', label: 'Dashboard', testId: 'bottom-nav-dashboard' },
  { id: 'court', label: 'Court', testId: 'bottom-nav-court' },
  { id: 'more', label: 'More', testId: 'bottom-nav-more' },
];

type Props = {
  view: PortalView;
  onNavigate: (view: PortalView) => void;
  onOpenMore: () => void;
  channelsUnread?: number;
};

export function BottomNav({ view, onNavigate, onOpenMore, channelsUnread = 0 }: Props) {
  return (
    <nav className="bottom-nav" aria-label="Mobile navigation" data-testid="bottom-nav">
      <div className="bottom-nav__items">
        {ITEMS.map((item) => (
          <button
            key={item.id}
            type="button"
            className={`bottom-nav__item${
              item.id === 'more'
                ? ['home', 'agents', 'skills', 'audit', 'settings', 'canvas'].includes(view)
                  ? ' is-active'
                  : ''
                : view === item.id
                  ? ' is-active'
                  : ''
            }`}
            data-testid={item.testId}
            onClick={() => (item.id === 'more' ? onOpenMore() : onNavigate(item.id))}
          >
            <span>
              {item.label}
              {item.id === 'channels' && channelsUnread > 0 ? (
                <span className="bottom-nav__unread-dot" aria-label="Unread channel messages" />
              ) : null}
            </span>
          </button>
        ))}
      </div>
    </nav>
  );
}
