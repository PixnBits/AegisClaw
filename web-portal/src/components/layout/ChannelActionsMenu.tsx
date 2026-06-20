import { useEffect, useRef, useState } from 'react';
import './ChannelActionsMenu.css';

type Props = {
  onArchive: () => void;
};

export function ChannelActionsMenu({ onArchive }: Props) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, [open]);

  return (
    <div className="channel-menu" ref={ref}>
      <button
        type="button"
        className="icon-button"
        aria-label="Channel options"
        aria-expanded={open}
        data-testid="channel-menu-button"
        onClick={() => setOpen(!open)}
      >
        ⋯
      </button>
      {open && (
        <div className="channel-menu__dropdown" role="menu">
          <button
            type="button"
            className="menu-item menu-item--muted-danger"
            role="menuitem"
            data-testid="archive-channel-button"
            onClick={() => {
              setOpen(false);
              onArchive();
            }}
          >
            Archive channel…
          </button>
        </div>
      )}
    </div>
  );
}