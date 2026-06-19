import { FormEvent, useCallback, useRef, useState } from 'react';
import { api } from '@/api/client';
import { Channel } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';
import { ActivityFeed } from '@/components/ActivityFeed/ActivityFeed';
import { AgentActivitySummary } from '@/components/AgentActivitySummary/AgentActivitySummary';
import { EmptyState } from '@/components/ui/EmptyState';
import { ChannelActionsMenu } from '@/components/layout/ChannelActionsMenu';
import { MentionPicker } from '@/components/channels/MentionPicker';
import { memberPersonas } from '@/components/layout/ContextPanel';
import { useIsMobile } from '@/hooks/useMediaQuery';
import './ChannelsView.css';

type Props = {
  onOpenCanvas: () => void;
  onOpenContext: () => void;
  onGoHome: () => void;
};

function mentionState(text: string, cursor: number): { active: boolean; query: string; start: number } {
  const before = text.slice(0, cursor);
  const at = before.lastIndexOf('@');
  if (at < 0) return { active: false, query: '', start: -1 };
  const fragment = before.slice(at + 1);
  if (/\s/.test(fragment)) return { active: false, query: '', start: -1 };
  return { active: true, query: fragment, start: at };
}

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
  const unreadByChannel = usePortalStore((s) => s.unreadByChannel);
  const loadChannels = usePortalStore((s) => s.loadChannels);
  const [content, setContent] = useState('');
  const [posting, setPosting] = useState(false);
  const [cursor, setCursor] = useState(0);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSelectChannel = (ch: Channel) => selectChannel(ch);

  const handleSwitcherChange = async (value: string) => {
    if (value === '__create__') {
      const id = window.prompt('New channel ID');
      if (!id?.trim()) return;
      await api.createChannel(id.trim());
      await loadChannels();
      await selectChannel({ id: id.trim(), members: [] });
      return;
    }
    const ch = channels.find((c) => c.id === value);
    if (ch) handleSelectChannel(ch);
  };

  const insertMention = useCallback(
    (role: string) => {
      const mention = mentionState(content, cursor);
      if (!mention.active) return;
      const next = `${content.slice(0, mention.start)}@${role} ${content.slice(cursor)}`;
      setContent(next);
      requestAnimationFrame(() => {
        const pos = mention.start + role.length + 2;
        textareaRef.current?.focus();
        textareaRef.current?.setSelectionRange(pos, pos);
        setCursor(pos);
      });
    },
    [content, cursor],
  );

  const showChannelSwitcher = isMobile || channels.length > 1;

  if (!currentChannel) {
    return (
      <section className="panel content-panel content-panel--channels" data-testid="channels-panel" data-page="channels">
        <EmptyState
          testId="channel-empty-state"
          eyebrow="Collaboration"
          title="Choose a workspace"
          description="Channels are calm, focused spaces where you and specialist agents collaborate under governance."
          hint="Pick a channel below, or start from Home with a natural-language goal."
          suggestions={[
            { label: 'Research a topic', onClick: onGoHome },
            { label: 'Start a feature', onClick: onGoHome },
            { label: 'Audit security', onClick: onGoHome },
          ]}
          action={
            <button type="button" className="primary-button" onClick={onGoHome}>
              Go to Command Center
            </button>
          }
        />
        {channels.length > 0 && (
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
  const mention = mentionState(content, cursor);

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
            {showChannelSwitcher ? (
              <select
                className="channel-header__select"
                value={currentChannel.id}
                aria-label="Switch channel"
                data-testid="channel-switcher"
                onChange={(e) => void handleSwitcherChange(e.target.value)}
              >
                {channels.map((ch) => (
                  <option key={ch.id} value={ch.id}>
                    {ch.id}
                    {(unreadByChannel[ch.id] || 0) > 0 && ch.id !== currentChannel.id
                      ? ` (${unreadByChannel[ch.id]})`
                      : ''}
                  </option>
                ))}
                {isMobile ? <option value="__create__">+ Create new channel…</option> : null}
              </select>
            ) : (
              <h2 className="channel-header__title" id="selectedChannelId">
                {currentChannel.id}
              </h2>
            )}
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

        {/* Mobile: compact activity strip stays above feed per spec */}
        {isMobile && (
          <div className="channel-primary" data-testid="channel-primary">
            <AgentActivitySummary
              harness={harness}
              tokenUsage={overviewStats?.token_usage?.channel}
              onDrillDown={onOpenCanvas}
              compact
              idlePersonas={idlePersonas}
            />
          </div>
        )}

        <ActivityFeed
          items={feed}
          channelId={currentChannel.id}
          onCollapseAll={() => collapseAllFeedReasoning(currentChannel.id)}
        />

        <form className="channel-input channel-input--with-mentions" onSubmit={handlePost} noValidate>
          <div className="channel-input__field">
            <label htmlFor="postContent" className="sr-only">
              Message
            </label>
            <MentionPicker
              open={mention.active}
              query={mention.query}
              members={currentChannel.members || []}
              onPick={insertMention}
              onClose={() => {}}
            />
            <textarea
              ref={textareaRef}
              id="postContent"
              rows={isMobile ? 3 : 2}
              maxLength={2000}
              placeholder="Message the channel — type @ to mention agents or teammates…"
              data-testid="message-input"
              value={content}
              onChange={(e) => {
                setContent(e.target.value);
                setCursor(e.target.selectionStart ?? e.target.value.length);
              }}
              onClick={(e) => setCursor(e.currentTarget.selectionStart ?? 0)}
              onKeyUp={(e) => setCursor(e.currentTarget.selectionStart ?? 0)}
            />
          </div>
          <button type="submit" className="primary-button" data-testid="send-button" disabled={posting}>
            {posting ? 'Sending…' : 'Post'}
          </button>
        </form>
      </article>
    </section>
  );
}
