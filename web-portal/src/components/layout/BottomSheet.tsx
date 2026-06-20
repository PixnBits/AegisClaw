import { ReactNode, useEffect } from 'react';

type Props = {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
};

export function BottomSheet({ open, title, onClose, children }: Props) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <>
      <div className="bottom-sheet-backdrop" onClick={onClose} aria-hidden="true" />
      <div className="bottom-sheet" role="dialog" aria-modal="true" aria-label={title} data-testid="bottom-sheet">
        <header className="bottom-sheet__header">
          <h3>{title}</h3>
          <button type="button" className="secondary-button" onClick={onClose} aria-label="Close">
            Close
          </button>
        </header>
        <div className="bottom-sheet__body">{children}</div>
      </div>
    </>
  );
}