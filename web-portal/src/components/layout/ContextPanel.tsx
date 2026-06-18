import { FormEvent, useState } from 'react';
import { api } from '@/api/client';
import { HarnessState } from '@/contracts';
import { groupMembers, memberRole } from '@/lib/members';
import { usePortalStore } from '@/store/portalStore';
import { PolicyPresetToggle } from '@/components/PolicyPreset/PolicyPresetToggle';

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
  const groups = groupMembers(members);

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
    if (!currentChannel || !confirm(`Remove ${r}?`)) return;
    await api.removeMember(currentChannel.id, r);
    await selectChannel(currentChannel);
    await loadChannels();
  };

  return (
    <aside className="panel context-panel" data-testid="context-panel">
      <div className="panel-heading">
        <p className="eyebrow">Context</p>
        <h2>Operator</h2>
      </div>

      <article className="subpanel" data-testid="harness-teaser">
        <p className="eyebrow">Harness</p>
        <p id="harnessTeaserGoal">
          {harness?.plan?.goal
            ? `${harness.plan.goal}${harness.tasks?.length ? ` — ${harness.tasks.length} active task(s)` : ''}`
            : 'No active plan — submit a goal to begin.'}
        </p>
      </article>

      {channelId && (
        <>
          <article className="subpanel">
            <p className="eyebrow">Reasoning Policy</p>
            <PolicyPresetToggle channelId={channelId} showInheritance />
          </article>

          <article className="subpanel">
            <div className="panel-heading--toolbar">
              <p className="eyebrow">Members</p>
              <button
                type="button"
                className="secondary-button"
                data-testid="toggle-invite-button"
                onClick={() => setShowInvite(!showInvite)}
              >
                Invite
              </button>
            </div>
            <ul className="list-stack" data-testid="members-list">
              {Object.entries(groups).map(([group, items]) =>
                items.length ? (
                  <li key={group} className="member-group">
                    <strong className="member-group__title">{group}</strong>
                    <ul className="list-stack compact-list">
                      {items.map((m) => {
                        const r = memberRole(m);
                        return (
                          <li key={r} className="list-card member-chip" data-testid={`member-${r}`}>
                            {r}
                            <button type="button" className="danger-button" onClick={() => handleRemove(r)}>
                              Remove
                            </button>
                          </li>
                        );
                      })}
                    </ul>
                  </li>
                ) : null,
              )}
            </ul>
            {showInvite && (
              <form className="inline-form invite-form" data-testid="add-member-form" onSubmit={handleAdd} noValidate>
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
          </article>
        </>
      )}

      <article className="subpanel">
        <p className="eyebrow">Security Posture</p>
        <ul className="list-stack subtle">
          <li>Browser isolated</li>
          <li>Stable selectors</li>
          <li>No external resources</li>
        </ul>
      </article>
    </aside>
  );
}