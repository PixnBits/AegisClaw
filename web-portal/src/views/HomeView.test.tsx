import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { usePortalStore } from '@/store/portalStore';
import { HomeView } from './HomeView';

vi.mock('@/api/client', () => ({
  api: {
    goals: vi.fn(),
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

describe('HomeView goal submit', () => {
  beforeEach(() => {
    installMatchMedia(false);
    vi.mocked(api.goals).mockReset();
    usePortalStore.setState({
      dashboard: null,
      planPreview: null,
      harnessByChannel: {},
      overviewStats: null,
    });
  });

  it('shows an alert and keeps the goal text when submit fails', async () => {
    vi.mocked(api.goals).mockRejectedValue(new Error('goal.submit: ensure PM for main: invalid vm id: refused'));
    render(<HomeView onOpenChannel={vi.fn()} onOpenCanvas={vi.fn()} />);
    fireEvent.change(screen.getByTestId('command-bar-input'), { target: { value: 'ship the thing' } });
    fireEvent.click(screen.getByTestId('command-bar-submit'));
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid vm id: refused');
    expect(screen.getByTestId('command-bar-input')).toHaveValue('ship the thing');
    expect(screen.getByTestId('command-bar-submit')).toBeEnabled();
    expect(screen.queryByTestId('plan-preview')).not.toBeInTheDocument();
  });
});
