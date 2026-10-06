import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { ContextPanel } from '@/components/layout/ContextPanel';
import { EVENT } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';
import { ChannelsView } from '@/views/ChannelsView';

// Every api function is a mock; unlisted ones resolve to {}.
vi.mock('@/api/client', () => {
  const fns: Record<string, ReturnType<typeof vi.fn>> = {};
  return {
    api: new Proxy(fns, {
      get: (target, key: string) => {
        if (!target[key]) target[key] = vi.fn(async () => ({}));
        return target[key];
      },
    }),
  };
});

// A valid channel id that the credential pattern matches in part. The
// portal API now returns it raw in the list, the detail and realtime.
const ID = 'task-refactorauthenticationmodule';

const detail = {
  id: ID,
  messages: [{ channel_id: ID, from: 'user', content: 'hello' }],
  members: [{ role: 'coder' }],
};

function installMatchMedia(matches: boolean) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: (query: string) => ({
      matches,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

describe('a key-like valid channel id routes everywhere', () => {
  beforeEach(() => {
    installMatchMedia(true);
    vi.mocked(api.channel).mockReset().mockResolvedValue(detail as never);
    vi.mocked(api.harness).mockReset().mockResolvedValue({ plan: null, tasks: [] } as never);
    vi.mocked(api.channels).mockReset().mockResolvedValue({ channels: [{ id: ID }] } as never);
    vi.mocked(api.postChannel).mockReset().mockResolvedValue({ ok: true } as never);
    vi.mocked(api.addMember).mockReset().mockResolvedValue({ ok: true } as never);
    vi.mocked(api.archiveChannel).mockReset().mockResolvedValue({ ok: true } as never);
    vi.mocked(api.goals).mockReset().mockResolvedValue({
      plan_id: `plan_${ID}`,
      channel_id: ID,
      goal: 'ship it',
      stages: [],
      preview: true,
    } as never);
    usePortalStore.setState({
      channels: [{ id: ID }],
      currentChannel: null,
      feedByChannel: {},
      harnessByChannel: {},
      unreadByChannel: {},
      view: 'channels',
    });
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  async function select() {
    await act(async () => {
      await usePortalStore.getState().selectChannel({ id: ID });
    });
  }

  it('select: current channel, feed and harness use the raw id', async () => {
    await select();
    const s = usePortalStore.getState();
    expect(api.channel).toHaveBeenCalledWith(ID);
    expect(api.harness).toHaveBeenCalledWith(ID);
    expect(s.currentChannel?.id).toBe(ID);
    expect(Object.keys(s.feedByChannel)).toEqual([ID]);
    expect(s.feedByChannel[ID]).toHaveLength(1);
  });

  it('post and feed refresh target the raw id', async () => {
    await select();
    vi.useFakeTimers();
    try {
      await act(async () => {
        await usePortalStore.getState().postMessage('hi');
      });
    } finally {
      vi.useRealTimers();
    }
    expect(api.postChannel).toHaveBeenCalledWith(ID, 'hi');
    expect(vi.mocked(api.channel).mock.calls.every((c) => c[0] === ID)).toBe(true);
    expect(usePortalStore.getState().currentChannel?.id).toBe(ID);
  });

  it('realtime channel.activity lands in the raw-id feed', async () => {
    await select();
    act(() => {
      usePortalStore.getState().handleRealtime({
        type: EVENT.channelActivity,
        channel_id: ID,
        event: { kind: 'message', from: 'coder', content: 'live reply' },
      });
    });
    expect(usePortalStore.getState().feedByChannel[ID]).toHaveLength(2);
    expect(usePortalStore.getState().unreadByChannel[ID] || 0).toBe(0);
  });

  it('members: invite targets the raw id', async () => {
    await select();
    render(<ContextPanel channelId={ID} />);
    fireEvent.click(screen.getByText('Members'));
    fireEvent.click(screen.getByTestId('toggle-invite-button'));
    fireEvent.change(screen.getByTestId('add-member-input'), { target: { value: 'tester' } });
    await act(async () => {
      fireEvent.click(screen.getByTestId('add-member-button'));
    });
    expect(api.addMember).toHaveBeenCalledWith(ID, 'tester');
  });

  it('archive targets the raw id', async () => {
    await select();
    render(<ChannelsView onOpenCanvas={vi.fn()} onOpenContext={vi.fn()} onGoHome={vi.fn()} />);
    fireEvent.click(screen.getByTestId('channel-menu-button'));
    await act(async () => {
      fireEvent.click(screen.getByTestId('archive-channel-button'));
    });
    expect(api.archiveChannel).toHaveBeenCalledWith(ID);
  });

  it('goal submit keys the harness on the raw id', async () => {
    await act(async () => {
      await usePortalStore.getState().submitGoal('ship it');
    });
    const h = usePortalStore.getState().harnessByChannel[ID];
    expect(h?.plan?.channel_id).toBe(ID);
  });
});
