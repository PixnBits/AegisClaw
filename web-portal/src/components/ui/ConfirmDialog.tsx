import { ReactNode, useEffect, useId } from 'react';
import './ConfirmDialog.css';

export type ConfirmVariant = 'primary' | 'danger';

type Props = {
  open: boolean;
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: ConfirmVariant;
  onConfirm: () => void;
  onCancel: () => void;
  testId?: string;
};

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  variant = 'primary',
  onConfirm,
  onCancel,
  testId = 'confirm-dialog',
}: Props) {
  const titleId = useId();

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onCancel]);

  if (!open) return null;

  const confirmClass = variant === 'danger' ? 'danger-button' : 'primary-button';

  return (
    <>
      <div className="confirm-dialog-backdrop" onClick={onCancel} aria-hidden="true" />
      <div
        className="confirm-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        data-testid={testId}
      >
        <h3 id={titleId} className="confirm-dialog__title">
          {title}
        </h3>
        <div className="confirm-dialog__message">{message}</div>
        <div className="confirm-dialog__actions">
          <button type="button" className="secondary-button" onClick={onCancel} data-testid={`${testId}-cancel`}>
            {cancelLabel}
          </button>
          <button type="button" className={confirmClass} onClick={onConfirm} data-testid={`${testId}-confirm`}>
            {confirmLabel}
          </button>
        </div>
      </div>
    </>
  );
}
