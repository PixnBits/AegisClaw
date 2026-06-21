import { ReactNode, useCallback, useEffect, useRef, useState } from 'react';
import './VirtualList.css';

/** Per performance-targets.md: virtualize once visible list exceeds ~100 items. */
export const VIRTUAL_LIST_THRESHOLD = 100;
const DEFAULT_ITEM_HEIGHT = 96;
const OVERSCAN = 6;

type Props<T> = {
  items: T[];
  renderItem: (item: T, index: number) => ReactNode;
  getKey: (item: T, index: number) => string;
  estimateItemHeight?: number;
  threshold?: number;
  className?: string;
  testId?: string;
  ariaLabel?: string;
};

export function VirtualList<T>({
  items,
  renderItem,
  getKey,
  estimateItemHeight = DEFAULT_ITEM_HEIGHT,
  threshold = VIRTUAL_LIST_THRESHOLD,
  className,
  testId,
  ariaLabel,
}: Props<T>) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [range, setRange] = useState({ start: 0, end: Math.min(items.length, 30) });
  const [scrollTop, setScrollTop] = useState(0);

  const updateRange = useCallback(() => {
    const el = containerRef.current;
    if (!el || items.length <= threshold) return;
    const top = el.scrollTop;
    const viewH = el.clientHeight || 400;
    const start = Math.max(0, Math.floor(top / estimateItemHeight) - OVERSCAN);
    const visible = Math.ceil(viewH / estimateItemHeight) + OVERSCAN * 2;
    const end = Math.min(items.length, start + visible);
    setScrollTop(top);
    setRange((prev) => (prev.start === start && prev.end === end ? prev : { start, end }));
  }, [estimateItemHeight, items.length, threshold]);

  useEffect(() => {
    updateRange();
  }, [items.length, updateRange]);

  if (items.length <= threshold) {
    return (
      <div className={className} data-testid={testId} aria-label={ariaLabel}>
        {items.map((item, i) => (
          <div key={getKey(item, i)}>{renderItem(item, i)}</div>
        ))}
      </div>
    );
  }

  const totalHeight = items.length * estimateItemHeight;
  const slice = items.slice(range.start, range.end);
  const offsetY = range.start * estimateItemHeight;

  return (
    <div
      ref={containerRef}
      className={`virtual-list${className ? ` ${className}` : ''}`}
      data-testid={testId}
      data-virtualized="true"
      aria-label={ariaLabel}
      onScroll={updateRange}
      role="log"
      tabIndex={0}
    >
      <div className="virtual-list__spacer" style={{ height: totalHeight }}>
        <div className="virtual-list__window" style={{ transform: `translateY(${offsetY}px)` }}>
          {slice.map((item, i) => {
            const index = range.start + i;
            return <div key={getKey(item, index)}>{renderItem(item, index)}</div>;
          })}
        </div>
      </div>
      <span className="virtual-list__meta subtle" data-testid={`${testId}-virtual-meta`} aria-hidden="true">
        Showing {range.start + 1}–{range.end} of {items.length}
        {scrollTop > 0 ? '' : ''}
      </span>
    </div>
  );
}