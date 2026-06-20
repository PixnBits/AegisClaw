import { ReactNode, useState } from 'react';
import './CollapsibleSection.css';

type Props = {
  title: string;
  count?: number;
  defaultOpen?: boolean;
  children: ReactNode;
  testId?: string;
};

export function CollapsibleSection({ title, count, defaultOpen = false, children, testId }: Props) {
  const [open, setOpen] = useState(defaultOpen);
  const id = testId || `collapse-${title.toLowerCase().replace(/\s+/g, '-')}`;

  return (
    <div className={`collapsible${open ? ' collapsible--open' : ''}`} data-testid={testId}>
      <button
        type="button"
        className="collapsible__trigger"
        aria-expanded={open}
        aria-controls={`${id}-body`}
        onClick={() => setOpen(!open)}
      >
        <span className="collapsible__chevron" aria-hidden="true">
          {open ? '▾' : '▸'}
        </span>
        <span className="collapsible__title">{title}</span>
        {count != null && <span className="collapsible__count">{count}</span>}
      </button>
      <div id={`${id}-body`} className="collapsible__body" hidden={!open}>
        {children}
      </div>
    </div>
  );
}