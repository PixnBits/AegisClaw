import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { VirtualList } from './VirtualList';

describe('VirtualList', () => {
  it('renders all items below threshold without virtualization', () => {
    const items = Array.from({ length: 5 }, (_, i) => ({ id: `item-${i}` }));
    render(
      <VirtualList
        items={items}
        getKey={(item) => item.id}
        testId="test-list"
        renderItem={(item) => <div data-testid={item.id}>{item.id}</div>}
      />,
    );
    expect(screen.getByTestId('item-0')).toBeInTheDocument();
    expect(screen.queryByTestId('test-list')).not.toHaveAttribute('data-virtualized');
  });

  it('virtualizes long lists', () => {
    const items = Array.from({ length: 150 }, (_, i) => ({ id: `row-${i}` }));
    render(
      <VirtualList
        items={items}
        getKey={(item) => item.id}
        testId="long-list"
        renderItem={(item) => <div>{item.id}</div>}
      />,
    );
    expect(screen.getByTestId('long-list')).toHaveAttribute('data-virtualized', 'true');
    expect(screen.getByTestId('long-list-virtual-meta')).toHaveTextContent('of 150');
  });
});