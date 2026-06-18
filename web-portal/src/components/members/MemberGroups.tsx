import { useState } from 'react';
import { Member, MemberGroup, groupMembers, memberRole } from '@/lib/members';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import './MemberGroups.css';

type Props = {
  members: Member[];
  onRemove: (role: string) => void;
};

const GROUP_DEFAULT_OPEN: Record<MemberGroup, boolean> = {
  Humans: false,
  'Project / SDLC': false,
  'Core Court': false,
};

export function MemberGroups({ members, onRemove }: Props) {
  const groups = groupMembers(members);
  const [expandedMember, setExpandedMember] = useState<string | null>(null);

  return (
    <div className="member-groups" data-testid="members-list">
      {(Object.entries(groups) as [MemberGroup, Member[]][]).map(([group, items]) =>
        items.length > 0 ? (
          <CollapsibleSection
            key={group}
            title={group}
            count={items.length}
            defaultOpen={GROUP_DEFAULT_OPEN[group]}
            testId={`member-group-${group.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '')}`}
          >
            <ul className="member-groups__list">
              {items.map((m) => {
                const role = memberRole(m);
                const showActions = expandedMember === role;
                return (
                  <li key={role} className="member-row" data-testid={`member-${role}`}>
                    <div className="member-row__info">
                      <span className="member-row__avatar" aria-hidden="true">
                        {role.charAt(0).toUpperCase()}
                      </span>
                      <span className="member-row__name">{role}</span>
                    </div>
                    <div className="member-row__actions">
                      <button
                        type="button"
                        className="icon-button"
                        aria-label={`Actions for ${role}`}
                        aria-expanded={showActions}
                        onClick={() => setExpandedMember(showActions ? null : role)}
                      >
                        ⋯
                      </button>
                      {showActions && (
                        <div className="member-row__menu">
                          <button type="button" className="menu-item menu-item--danger" onClick={() => onRemove(role)}>
                            Remove
                          </button>
                        </div>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          </CollapsibleSection>
        ) : null,
      )}
    </div>
  );
}