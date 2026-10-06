import { FormEvent, useState } from 'react';
import { api } from '@/api/client';
import { InvalidChannelBadge } from '@/components/channels/InvalidChannelBadge';
import { Channel } from '@/contracts';
import { validateChannelId } from '@/lib/channelId';
import { usePortalStore } from '@/store/portalStore';

type Props = {
  channels: Channel[];
  currentChannelId?: string;
  onSelect: (ch: Channel) => void;
  onNavigate: (view: import('@/contracts').PortalView) => void;
};

export function Sidebar({ channels, currentChannelId, onSelect, onNavigate }: Props) {
  const loadChannels = usePortalStore((s) => s.loadChannels);
  const unreadByChannel = usePortalStore((s) => s.unreadByChannel);
  const [newId, setNewId] = useState('');
  const [createError, setCreateError] = useState('');

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault();
    const id = newId.trim();
    if (!id) return;
    const invalid = validateChannelId(id);
    if (invalid) {
      setCreateError(invalid);
      return;
    }
    setCreateError('');
    try {
      await api.createChannel(id);
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'Could not create channel');
      return;
    }
    setNewId('');
    await loadChannels();
  };

  return (
    <aside className="panel sidebar-panel" data-testid="channels-sidebar">
      <div className="panel-heading">
        <div>
          <p className="eyebrow">Collaboration</p>
          <h2>Channels</h2>
        </div>
      </div>
      <ul className="list-stack compact-list" data-testid="channels-list">
        {channels.map((ch) => (
          <li key={ch.id}>
            <button
              type="button"
              className={`list-card channel-list-item${currentChannelId === ch.id ? ' active' : ''}`}
              onClick={() => onSelect(ch)}
            >
              <span className="channel-list-item__label">
                {ch.id}
                <InvalidChannelBadge idValid={ch.id_valid} idError={ch.id_error} />
                {(unreadByChannel[ch.id] || 0) > 0 && currentChannelId !== ch.id ? (
                  <span className="channel-unread-badge" aria-label="Unread messages">
                    {unreadByChannel[ch.id] > 9 ? '9+' : unreadByChannel[ch.id]}
                  </span>
                ) : null}
              </span>
              <small className="subtle">{(ch.members || []).length} members</small>
            </button>
          </li>
        ))}
      </ul>
      <form className="inline-form" onSubmit={handleCreate} noValidate>
        <input
          type="text"
          placeholder="new-channel-id"
          value={newId}
          onChange={(e) => {
            setNewId(e.target.value);
            if (createError) setCreateError('');
          }}
          aria-invalid={createError ? true : undefined}
          required
        />
        <button type="submit" className="primary-button" data-testid="create-channel-button">
          Add
        </button>
      </form>
      {createError ? (
        <p className="form-error" role="alert" data-testid="create-channel-error">
          {createError}
        </p>
      ) : null}
      <div className="panel-subsection">
        <p className="eyebrow">Quick Actions</p>
        <div className="button-stack">
          <button type="button" className="primary-button" data-testid="new-channel-button">
            New Channel
          </button>
          <button type="button" className="secondary-button" onClick={() => onNavigate('skills')}>
            Propose Skill
          </button>
        </div>
      </div>
    </aside>
  );
}