import { FormEvent, useState } from 'react';
import { api } from '@/api/client';
import { HarnessState } from '@/contracts';
import { memberRole } from '@/lib/members';
import { usePortalStore } from '@/store/portalStore';
import { PolicyPresetToggle } from '@/components/PolicyPreset/PolicyPresetToggle';
import { MemberGroups } from '@/components/members/MemberGroups';
import { CollapsibleSection } from '@/components/ui/CollapsibleSection';
import './ContextPanel.css';

type Props = {
  harness?: HarnessState | null;
  channelId?: string;
  collapsed?: boolean;
};

export function ContextPanel({ harness, channelId, collapsed }: Props) {
  const currentChannel = usePortalStore((s) => s.currentChannel);
  const selectChannel = usePortalStore((s) => s.selectChannel);
  const loadChannels = usePortalStore((s) => s.loadChannels);
  const [showInvite, setShowInvite] = useState(false);
  const [role, setRole] = useState('');

  if (collapsed) return null;

  const members = currentChannel?.members || [];

  const handleAdd = async (e: FormEvent) => {
    e.preventDefault();
    if (!currentChannel || !role.trim()) return;
    await api.addMember(currentChannel.id, role.trim());
    setRole('');
    setShowInvite(false);
    await selectChannel(currentChannel);
    await loadChannels();
  };

  const handleRemove = async (r: string) => {
    if (!currentChannel || !confirm(`Remove ${r} from this channel?`)) return;
    await api.removeMember(currentChannel.id, r);
    await selectChannel(currentChannel);
    await loadChannels();
  };

  return (
    <aside className="panel context-panel" data-testid="context-panel">
      <div className="context-panel__heading">
        <p className="eyebrow">Context</p>
        <h2 className="context-panel__title">Operator</h2>
      </div>

      <CollapsibleSection title="Harness" defaultOpen>
        <p className="context-panel__body-text" id="harnessTeaserGoal" data-testid="harness-teaser">
          {harness?.plan?.goal
            ? `${harness.plan.goal}${harness.tasks?.length ? ` — ${harness.tasks.length} active task(s)` : ''}`
            : 'No active plan yet. Use the command bar on Home to submit a goal.'}
        </p>
      </CollapsibleSection>

      {channelId && (
        <>
          <CollapsibleSection title="Reasoning policy" defaultOpen>
            <PolicyPresetToggle channelId={channelId} showInheritance />
          </CollapsibleSection>

          <CollapsibleSection
            title="Members"
            count={members.length}
            defaultOpen={false}
            testId="members-section"
          >
            <div className="context-panel__toolbar">
              <button
                type="button"
                className="secondary-button secondary-button--small"
                data-testid="toggle-invite-button"
                onClick={() => setShowInvite(!showInvite)}
              >
                {showInvite ? 'Cancel' : 'Invite'}
              </button>
            </div>
            <MemberGroups members={members} onRemove={handleRemove} />
            {showInvite && (
              <form className="invite-form" data-testid="add-member-form" onSubmit={handleAdd} noValidate>
                <input
                  type="text"
                  placeholder="coder or user:alice"
                  value={role}
                  onChange={(e) => setRole(e.target.value)}
                  required
                  data-testid="add-member-input"
                />
                <button type="submit" className="secondary-button" data-testid="add-member-button">
                  Add
                </button>
              </form>
            )}
          </CollapsibleSection>
        </>
      )}

      <CollapsibleSection title="Security posture" defaultOpen={false}>
        <ul className="context-panel__list subtle">
          <li>Browser isolated</li>
          <li>Stable selectors</li>
          <li>No external resources</li>
        </ul>
      </CollapsibleSection>
    </aside>
  );
}

/** Extract persona labels from channel members for activity summary fallback */
export function memberPersonas(members: { role?: string; agent_id?: string }[]): string[] {
  return members.map(memberRole).filter((r) => !r.startsWith('user'));
}