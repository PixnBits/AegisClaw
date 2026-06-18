import { FormEvent, useState } from 'react';
import { api } from '@/api/client';
import { Channel } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';
import { AgentActivitySummary } from '@/components/AgentActivitySummary/AgentActivitySummary';
import { CompactHarness } from '@/components/CompactHarness/CompactHarness';
import { ActivityFeed } from '@/components/ActivityFeed/ActivityFeed';
import { PolicyPresetToggle } from '@/components/PolicyPreset/PolicyPresetToggle';
import { useIsMobile } from '@/hooks/useMediaQuery';

type Props = {
  onOpenCanvas: () => void;
  onOpenContext: () => void;
};

export function ChannelsView({ onOpenCanvas, onOpenContext }: Props) {
  const isMobile = useIsMobile();
  const channels = usePortalStore((s) => s.channels);
  const currentChannel = usePortalStore((s) => s.currentChannel);
  const selectChannel = usePortalStore((s) => s.selectChannel);
  const harnessByChannel = usePortalStore((s) => s.harnessByChannel);
  const feedByChannel = usePortalStore((s) => s.feedByChannel);
  const postMessage = usePortalStore((s) => s.postMessage);
  const collapseAllFeedReasoning = usePortalStore((s) => s.collapseAllFeedReasoning);
  const overviewStats = usePortalStore((s) => s.overviewStats);
  const [content, setContent] = useState('');
  const [posting, setPosting] = useState(false);

  const handleSelectChannel = (ch: Channel) => selectChannel(ch);

  if (!currentChannel) {
    return (
      <section className="panel content-panel content-panel--channels" data-testid="channels-panel" data-page="channels">
        <div className="channel-empty-state" data-testid="channel-empty-state">
          <p className="eyebrow">Channels</p>
          <h2>Select a channel to begin</h2>
          <p className="subtle">Give the PM a goal to get started, or pick a channel below.</p>
        </div>
        {isMobile && (
          <ul className="list-stack compact-list mobile-channel-list" data-testid="channels-list">
            {channels.map((ch) => (
              <li
                key={ch.id}
                className="list-card"
                onClick={() => handleSelectChannel(ch)}
                role="button"
                tabIndex={0}
              >
                <span>{ch.id}</span>
                <small className="subtle">{(ch.members || []).length} members</small>
              </li>
            ))}
          </ul>
        )}
      </section>
    );
  }

  const harness = harnessByChannel[currentChannel.id];
  const feed = feedByChannel[currentChannel.id] || [];

  const handlePost = async (e: FormEvent) => {
    e.preventDefault();
    const text = content.trim();
    if (!text) return;
    setPosting(true);
    try {
      await postMessage(text);
      setContent('');
    } finally {
      setPosting(false);
    }
  };

  const handleArchive = async () => {
    if (!confirm('Archive this channel?')) return;
    await api.archiveChannel(currentChannel.id);
    usePortalStore.setState({ currentChannel: null });
    await usePortalStore.getState().loadChannels();
  };

  return (
    <section className="panel content-panel content-panel--channels" data-testid="channels-panel" data-page="channels">
      <article className="channel-workspace" data-testid="channel-detail">
        <header className="channel-header">
          <div>
            <p className="eyebrow">Channel</p>
            <h2 id="selectedChannelId">{currentChannel.id}</h2>
          </div>
          <div className="channel-header__actions">
            {isMobile && (
              <button type="button" className="secondary-button" onClick={onOpenContext}>
                Context
              </button>
            )}
            <button type="button" className="danger-button" data-testid="archive-channel-button" onClick={handleArchive}>
              Archive
            </button>
          </div>
        </header>

        <AgentActivitySummary
          harness={harness}
          tokenUsage={overviewStats?.token_usage?.channel}
          onDrillDown={onOpenCanvas}
          compact={isMobile}
        />

        {isMobile && <PolicyPresetToggle channelId={currentChannel.id} />}

        <CompactHarness state={harness} onOpenCanvas={onOpenCanvas} />

        <ActivityFeed
          items={feed}
          channelId={currentChannel.id}
          onCollapseAll={() => collapseAllFeedReasoning(currentChannel.id)}
        />

        <form className="channel-input" onSubmit={handlePost} noValidate>
          <textarea
            id="postContent"
            rows={2}
            maxLength={2000}
            placeholder="Post to channel…"
            data-testid="message-input"
            value={content}
            onChange={(e) => setContent(e.target.value)}
          />
          <button type="submit" className="primary-button" data-testid="send-button" disabled={posting}>
            Post
          </button>
        </form>
      </article>
    </section>
  );
}