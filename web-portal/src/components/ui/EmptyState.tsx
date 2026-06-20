import { ReactNode } from 'react';
import './EmptyState.css';

type Props = {
  eyebrow?: string;
  title: string;
  description: string;
  hint?: string;
  action?: ReactNode;
  suggestions?: { label: string; onClick?: () => void }[];
  testId?: string;
};

export function EmptyState({ eyebrow, title, description, hint, action, suggestions, testId }: Props) {
  return (
    <div className="empty-state" data-testid={testId}>
      <div className="empty-state__icon" aria-hidden="true">
        <span className="empty-state__icon-inner" />
      </div>
      {eyebrow && <p className="eyebrow">{eyebrow}</p>}
      <h2 className="empty-state__title">{title}</h2>
      <p className="empty-state__description">{description}</p>
      {hint && <p className="empty-state__hint">{hint}</p>}
      {suggestions && suggestions.length > 0 && (
        <div className="empty-state__suggestions">
          <p className="empty-state__suggestions-label">Try starting with</p>
          <div className="empty-state__chips">
            {suggestions.map((s) => (
              <button
                key={s.label}
                type="button"
                className="empty-state__chip"
                onClick={s.onClick}
              >
                {s.label}
              </button>
            ))}
          </div>
        </div>
      )}
      {action && <div className="empty-state__action">{action}</div>}
    </div>
  );
}