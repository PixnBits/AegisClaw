import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { usePortalStore } from '@/store/portalStore';
import { Sidebar } from './Sidebar';

vi.mock('@/api/client', () => ({
  api: {
    createChannel: vi.fn(),
    channels: vi.fn(),
  },
}));

describe('Sidebar create channel', () => {
  beforeEach(() => {
    vi.mocked(api.createChannel).mockReset();
    vi.mocked(api.channels).mockReset();
    vi.mocked(api.channels).mockResolvedValue({ channels: [] });
  });

  it('shows a badge for an invalid stored id', () => {
    render(
      <Sidebar
        channels={[{ id: 'MyProj', id_valid: false, id_error: 'invalid channel id: must match' }]}
        onSelect={vi.fn()}
        onNavigate={vi.fn()}
      />,
    );
    const badge = screen.getByTestId('invalid-id-badge');
    expect(badge).toHaveTextContent('invalid id');
    expect(badge).toHaveAttribute('title', 'invalid channel id: must match');
  });

  it('does not call the api for an invalid id and shows the message', async () => {
    render(<Sidebar channels={[]} onSelect={vi.fn()} onNavigate={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText('new-channel-id'), { target: { value: 'MyProj' } });
    fireEvent.click(screen.getByTestId('create-channel-button'));
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid channel id');
    expect(api.createChannel).not.toHaveBeenCalled();
    expect(usePortalStore.getState().channels).toEqual([]);
  });

  it('calls api.createChannel for a valid id', async () => {
    vi.mocked(api.createChannel).mockResolvedValue({ id: 'plan-demo' });
    render(<Sidebar channels={[]} onSelect={vi.fn()} onNavigate={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText('new-channel-id'), { target: { value: 'plan-demo' } });
    fireEvent.click(screen.getByTestId('create-channel-button'));
    expect(await screen.findByDisplayValue('')).toBeTruthy();
    expect(api.createChannel).toHaveBeenCalledWith('plan-demo');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows the server message when create is rejected', async () => {
    vi.mocked(api.createChannel).mockRejectedValue(
      new Error('invalid channel id: must match ^[a-z][a-z0-9-]*$ and be <= 45 chars'),
    );
    render(<Sidebar channels={[]} onSelect={vi.fn()} onNavigate={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText('new-channel-id'), { target: { value: 'plan-demo' } });
    fireEvent.click(screen.getByTestId('create-channel-button'));
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid channel id: must match');
    expect(screen.getByPlaceholderText('new-channel-id')).toHaveValue('plan-demo');
  });
});
