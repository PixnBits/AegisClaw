import { FormEvent, useState } from 'react';
import { api } from '@/api/client';
import { Channel } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';

type Props = {
  channels: Channel[];
  currentChannelId?: string;
  onSelect: (ch: Channel) => void;
  onNavigate: (view: import('@/contracts').PortalView) => void;
};

export function Sidebar({ channels, currentChannelId, onSelect, onNavigate }: Props) {
  const loadChannels = usePortalStore((s) => s.loadChannels);
  const [newId, setNewId] = useState('');

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault();
    const id = newId.trim();
    if (!id) return;
    await api.createChannel(id);
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
              <span>{ch.id}</span>
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
          onChange={(e) => setNewId(e.target.value)}
          required
        />
        <button type="submit" className="primary-button" data-testid="create-channel-button">
          Add
        </button>
      </form>
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