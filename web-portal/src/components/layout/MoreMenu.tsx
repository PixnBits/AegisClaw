import { PortalView } from '@/contracts';
import { BottomSheet } from './BottomSheet';
import './MoreMenu.css';

const MORE_ITEMS: { id: PortalView; label: string; description: string }[] = [
  { id: 'home', label: 'Home', description: 'Command center and goal input' },
  { id: 'canvas', label: 'Canvas', description: 'Pipeline and parallel task view' },
  { id: 'agents', label: 'Agents', description: 'Fleet status and traces' },
  { id: 'skills', label: 'Skills', description: 'Proposals and governed capabilities' },
  { id: 'audit', label: 'Audit', description: 'Activity and compliance log' },
  { id: 'settings', label: 'Settings', description: 'Policy presets and preferences' },
];

type Props = {
  open: boolean;
  onClose: () => void;
  onNavigate: (view: PortalView) => void;
};

export function MoreMenu({ open, onClose, onNavigate }: Props) {
  return (
    <BottomSheet open={open} title="More" onClose={onClose}>
      <nav className="more-menu" aria-label="Additional views" data-testid="more-menu">
        {MORE_ITEMS.map((item) => (
          <button
            key={item.id}
            type="button"
            className="more-menu__item"
            data-testid={`more-nav-${item.id}`}
            onClick={() => {
              onNavigate(item.id);
              onClose();
            }}
          >
            <span className="more-menu__label">{item.label}</span>
            <span className="more-menu__desc subtle">{item.description}</span>
          </button>
        ))}
      </nav>
    </BottomSheet>
  );
}
