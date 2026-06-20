import { PortalView, ViewContext, topicsForView } from '@/contracts';

const NULL = '\x00';

export type ConnectionMode = 'disconnected' | 'connecting' | 'stomp' | 'sse-fallback';

export type RealtimeHandlers = {
  onMessage: (payload: Record<string, unknown>) => void;
  onStatus: (mode: ConnectionMode) => void;
};

export class RealtimeClient {
  private ws: WebSocket | null = null;
  private eventSource: EventSource | null = null;
  private subscriptions = new Map<string, string>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  mode: ConnectionMode = 'disconnected';

  constructor(private handlers: RealtimeHandlers) {}

  connect(): void {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    this.setMode('connecting');
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    this.ws = new WebSocket(`${proto}//${location.host}/stomp`);
    this.ws.onopen = () => {
      this.ws?.send(`CONNECT\naccept-version:1.2\nheart-beat:10000,10000\n\n${NULL}`);
    };
    this.ws.onmessage = (ev) => this.handleStompFrame(String(ev.data || ''));
    this.ws.onclose = () => {
      this.ws = null;
      this.ensureFallbackTransport();
      if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
      this.reconnectTimer = setTimeout(() => this.connect(), 5000);
    };
    this.ws.onerror = () => {
      // onclose follows; avoid claiming SSE until EventSource is actually open.
    };
  }

  /** Prefer live STOMP, then open SSE; otherwise disconnected. */
  private ensureFallbackTransport(): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      return;
    }
    if (this.eventSource?.readyState === EventSource.OPEN) {
      this.setMode('sse-fallback');
      return;
    }
    this.startSSE();
    if (this.eventSource?.readyState !== EventSource.OPEN) {
      this.setMode('disconnected');
    }
  }

  private setMode(mode: ConnectionMode): void {
    if (this.mode === mode) return;
    this.mode = mode;
    this.handlers.onStatus(mode);
  }

  private handleStompFrame(raw: string): void {
    if (raw.startsWith('CONNECTED')) {
      this.setMode('stomp');
      this.stopSSE();
      for (const [topic, subId] of this.subscriptions) {
        this.sendSubscribe(topic, subId);
      }
      return;
    }
    if (!raw.startsWith('MESSAGE')) return;
    const bodyIdx = raw.indexOf('\n\n');
    const body = bodyIdx >= 0 ? raw.slice(bodyIdx + 2).replace(NULL, '') : '';
    try {
      const payload = JSON.parse(body) as Record<string, unknown>;
      this.handlers.onMessage(payload);
    } catch {
      this.handlers.onMessage({ type: 'raw', body });
    }
  }

  private sendSubscribe(topic: string, subId: string): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(`SUBSCRIBE\nid:${subId}\ndestination:${topic}\n\n${NULL}`);
  }

  setViewTopics(view: PortalView, ctx: ViewContext = {}): void {
    const desired = topicsForView(view, ctx);
    const desiredSet = new Set(desired);
    for (const topic of [...this.subscriptions.keys()]) {
      if (!desiredSet.has(topic)) {
        const subId = this.subscriptions.get(topic);
        if (this.ws?.readyState === WebSocket.OPEN && subId) {
          this.ws.send(`UNSUBSCRIBE\nid:${subId}\n\n${NULL}`);
        }
        this.subscriptions.delete(topic);
      }
    }
    desired.forEach((topic, i) => {
      if (!this.subscriptions.has(topic)) {
        const subId = `sub-${view}-${i}`;
        this.subscriptions.set(topic, subId);
        this.sendSubscribe(topic, subId);
      }
    });
  }

  private startSSE(): void {
    if (this.eventSource) return;
    this.eventSource = new EventSource('/events');
    this.eventSource.onopen = () => {
      if (this.ws?.readyState !== WebSocket.OPEN) {
        this.setMode('sse-fallback');
      }
    };
    this.eventSource.onerror = () => {
      if (this.ws?.readyState === WebSocket.OPEN || this.ws?.readyState === WebSocket.CONNECTING) {
        return;
      }
      this.setMode('disconnected');
    };
    this.eventSource.onmessage = (ev) => {
      try {
        const payload = JSON.parse(ev.data) as Record<string, unknown>;
        this.handlers.onMessage(payload);
      } catch {
        /* ignore non-JSON */
      }
    };
  }

  private stopSSE(): void {
    this.eventSource?.close();
    this.eventSource = null;
  }

  disconnect(): void {
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.stopSSE();
    if (this.ws) {
      try {
        this.ws.send(`DISCONNECT\n\n${NULL}`);
        this.ws.close();
      } catch {
        /* ignore */
      }
      this.ws = null;
    }
    this.subscriptions.clear();
    this.setMode('disconnected');
  }
}