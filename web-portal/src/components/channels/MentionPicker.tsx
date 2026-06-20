import { useEffect, useRef } from 'react';
import { formatPersonaLabel } from '@/lib/display';
import { memberRole } from '@/lib/members';
import './MentionPicker.css';

type Props = {
  open: boolean;
  query: string;
  members: { role?: string; agent_id?: string }[];
  onPick: (mention: string) => void;
  onClose: () => void;
};

export function MentionPicker({ open, query, members, onPick, onClose }: Props) {
  const ref = useRef<HTMLDivElement>(null);

  const options = members
    .map((m) => memberRole(m))
    .filter((role, i, arr) => arr.indexOf(role) === i)
    .filter((role) => role.toLowerCase().includes(query.toLowerCase()))
    .slice(0, 8);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open, onClose]);

  if (!open || options.length === 0) return null;

  return (
    <div className="mention-picker" ref={ref} role="listbox" aria-label="Mention suggestions" data-testid="mention-picker">
      {options.map((role) => (
        <button
          key={role}
          type="button"
          className="mention-picker__item"
          role="option"
          data-testid={`mention-option-${role}`}
          onClick={() => onPick(role)}
        >
          <span className="mention-picker__label">{formatPersonaLabel(role)}</span>
          <span className="mention-picker__role subtle">@{role}</span>
        </button>
      ))}
    </div>
  );
}
