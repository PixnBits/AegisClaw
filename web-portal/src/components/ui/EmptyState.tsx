import { ReactNode } from 'react';
import './EmptyState.css';

type Props = {
  eyebrow?: string;
  title: string;
  description: string;
  hint?: string;
  action?: ReactNode;
  testId?: string;
};

export function EmptyState({ eyebrow, title, description, hint, action, testId }: Props) {
  return (
    <div className="empty-state" data-testid={testId}>
      <div className="empty-state__icon" aria-hidden="true">
        ◇
      </div>
      {eyebrow && <p className="eyebrow">{eyebrow}</p>}
      <h2 className="empty-state__title">{title}</h2>
      <p className="empty-state__description">{description}</p>
      {hint && <p className="empty-state__hint">{hint}</p>}
      {action && <div className="empty-state__action">{action}</div>}
    </div>
  );
}