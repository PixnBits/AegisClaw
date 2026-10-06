import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { usePortalStore } from '@/store/portalStore';
import { ChannelsView } from './ChannelsView';

vi.mock('@/api/client', () => ({
  api: {
    createChannel: vi.fn(),
    channels: vi.fn(),
    channel: vi.fn(),
    harness: vi.fn(),
    archiveChannel: vi.fn(),
  },
}));

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

describe('ChannelsView create channel', () => {
  const selectChannel = vi.fn(async () => {});
  const loadChannels = vi.fn(async () => {});

  beforeEach(() => {
    installMatchMedia(true);
    selectChannel.mockClear();
    loadChannels.mockClear();
    vi.mocked(api.createChannel).mockReset();
    usePortalStore.setState({
      channels: [{ id: 'main', members: [] }],
      currentChannel: { id: 'main', members: [] },
      harnessByChannel: {},
      feedByChannel: {},
      unreadByChannel: {},
      overviewStats: null,
      selectChannel,
      loadChannels,
    });
    vi.spyOn(window, 'prompt').mockReturnValue('Bad_ID');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows an alert and does not select when create fails', async () => {
    vi.mocked(api.createChannel).mockRejectedValue(new Error('invalid channel id: store refused'));
    vi.mocked(window.prompt).mockReturnValue('plan-demo');
    render(<ChannelsView onOpenCanvas={vi.fn()} onOpenContext={vi.fn()} onGoHome={vi.fn()} />);
    fireEvent.change(screen.getByTestId('channel-switcher'), { target: { value: '__create__' } });
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid channel id: store refused');
    expect(api.createChannel).toHaveBeenCalledWith('plan-demo');
    expect(selectChannel).not.toHaveBeenCalled();
    expect(loadChannels).not.toHaveBeenCalled();
    expect(usePortalStore.getState().currentChannel?.id).toBe('main');
  });

  it('shows the client-side message and does not call the api for an invalid id', async () => {
    render(<ChannelsView onOpenCanvas={vi.fn()} onOpenContext={vi.fn()} onGoHome={vi.fn()} />);
    fireEvent.change(screen.getByTestId('channel-switcher'), { target: { value: '__create__' } });
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid channel id');
    expect(api.createChannel).not.toHaveBeenCalled();
    expect(selectChannel).not.toHaveBeenCalled();
  });
});
