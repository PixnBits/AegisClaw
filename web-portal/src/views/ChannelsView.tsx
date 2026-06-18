import { FormEvent, useState } from 'react';
import { api } from '@/api/client';
import { Channel } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';
import { AgentActivitySummary } from '@/components/AgentActivitySummary/AgentActivitySummary';
import { CompactHarness } from '@/components/CompactHarness/CompactHarness';
import { ActivityFeed } from '@/components/ActivityFeed/ActivityFeed';
import { EmptyState } from '@/components/ui/EmptyState';
import { ChannelActionsMenu } from '@/components/layout/ChannelActionsMenu';
import { memberPersonas } from '@/components/layout/ContextPanel';
import { useIsMobile } from '@/hooks/useMediaQuery';
import './ChannelsView.css';

type Props = {
  onOpenCanvas: () => void;
  onOpenContext: () => void;
  onGoHome: () => void;
};

export function ChannelsView({ onOpenCanvas, onOpenContext, onGoHome }: Props) {
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
        <EmptyState
          testId="channel-empty-state"
          eyebrow="Collaboration"
          title="Choose a workspace"
          description="Channels are where you and specialist agents collaborate under governance. Pick one to see the harness, activity feed, and Court proposals."
          hint="Tip: Start from Home with a natural-language goal — the PM will decompose it into narrow tasks."
          action={
            <button type="button" className="primary-button" onClick={onGoHome}>
              Go to Command Center
            </button>
          }
        />
        {isMobile && channels.length > 0 && (
          <ul className="mobile-channel-list" data-testid="channels-list">
            {channels.map((ch) => (
              <li key={ch.id}>
                <button type="button" className="mobile-channel-card" onClick={() => handleSelectChannel(ch)}>
                  <span className="mobile-channel-card__name">{ch.id}</span>
                  <span className="subtle">{(ch.members || []).length} members</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    );
  }

  const harness = harnessByChannel[currentChannel.id];
  const feed = feedByChannel[currentChannel.id] || [];
  const idlePersonas = memberPersonas(currentChannel.members || []);

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
    if (!confirm('Archive this channel? Members will lose access to its history.')) return;
    await api.archiveChannel(currentChannel.id);
    usePortalStore.setState({ currentChannel: null });
    await usePortalStore.getState().loadChannels();
  };

  return (
    <section className="panel content-panel content-panel--channels" data-testid="channels-panel" data-page="channels">
      <article className="channel-workspace" data-testid="channel-detail">
        <header className="channel-header">
          <div className="channel-header__identity">
            <p className="eyebrow">Channel</p>
            <h2 className="channel-header__title" id="selectedChannelId">
              {currentChannel.id}
            </h2>
          </div>
          <div className="channel-header__actions">
            {isMobile && (
              <button type="button" className="secondary-button secondary-button--small" onClick={onOpenContext}>
                Context
              </button>
            )}
            <ChannelActionsMenu onArchive={handleArchive} />
          </div>
        </header>

        <AgentActivitySummary
          harness={harness}
          tokenUsage={overviewStats?.token_usage?.channel}
          onDrillDown={onOpenCanvas}
          compact={isMobile}
          idlePersonas={idlePersonas}
        />

        <CompactHarness state={harness} onOpenCanvas={onOpenCanvas} compactTasks={isMobile} />

        <ActivityFeed
          items={feed}
          channelId={currentChannel.id}
          onCollapseAll={() => collapseAllFeedReasoning(currentChannel.id)}
        />

        <form className="channel-input" onSubmit={handlePost} noValidate>
          <label htmlFor="postContent" className="sr-only">
            Message
          </label>
          <textarea
            id="postContent"
            rows={isMobile ? 3 : 2}
            maxLength={2000}
            placeholder="Message the channel — use @mentions for agents or teammates…"
            data-testid="message-input"
            value={content}
            onChange={(e) => setContent(e.target.value)}
          />
          <button type="submit" className="primary-button" data-testid="send-button" disabled={posting}>
            {posting ? 'Sending…' : 'Post'}
          </button>
        </form>
      </article>
    </section>
  );
}