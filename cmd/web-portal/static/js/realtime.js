// STOMP-over-WebSocket with SSE fallback (real-time-contracts.md).

import { topicsForView } from './contracts.js';

const NULL = '\x00';

export class RealtimeClient {
  constructor({ onMessage, onStatus }) {
    this.onMessage = onMessage || (() => {});
    this.onStatus = onStatus || (() => {});
    this.ws = null;
    this.eventSource = null;
    this.subscriptions = new Map();
    this.mode = 'disconnected';
    this._reconnectTimer = null;
  }

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    this.ws = new WebSocket(`${proto}//${location.host}/stomp`);
    this.ws.onopen = () => {
      this.ws.send(`CONNECT\naccept-version:1.2\nheart-beat:10000,10000\n\n${NULL}`);
    };
    this.ws.onmessage = (ev) => this._handleStompFrame(String(ev.data || ''));
    this.ws.onclose = () => {
      this._setMode('sse-fallback');
      this._startSSE();
      clearTimeout(this._reconnectTimer);
      this._reconnectTimer = setTimeout(() => this.connect(), 5000);
    };
    this.ws.onerror = () => {
      this._setMode('sse-fallback');
      this._startSSE();
    };
  }

  _setMode(mode) {
    if (this.mode === mode) return;
    this.mode = mode;
    this.onStatus(mode);
  }

  _handleStompFrame(raw) {
    if (raw.startsWith('CONNECTED')) {
      this._setMode('stomp');
      this._stopSSE();
      for (const [topic, subId] of this.subscriptions) {
        this._sendSubscribe(topic, subId);
      }
      return;
    }
    if (!raw.startsWith('MESSAGE')) return;
    const bodyIdx = raw.indexOf('\n\n');
    const body = bodyIdx >= 0 ? raw.slice(bodyIdx + 2).replace(NULL, '') : '';
    try {
      const payload = JSON.parse(body);
      this.onMessage(payload);
    } catch {
      this.onMessage({ type: 'raw', body });
    }
  }

  _sendSubscribe(topic, subId) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(`SUBSCRIBE\nid:${subId}\ndestination:${topic}\n\n${NULL}`);
  }

  setViewTopics(view, ctx = {}) {
    const desired = topicsForView(view, ctx);
    const desiredSet = new Set(desired);
    for (const topic of [...this.subscriptions.keys()]) {
      if (!desiredSet.has(topic)) {
        const subId = this.subscriptions.get(topic);
        if (this.ws?.readyState === WebSocket.OPEN) {
          this.ws.send(`UNSUBSCRIBE\nid:${subId}\n\n${NULL}`);
        }
        this.subscriptions.delete(topic);
      }
    }
    desired.forEach((topic, i) => {
      if (!this.subscriptions.has(topic)) {
        const subId = `sub-${view}-${i}`;
        this.subscriptions.set(topic, subId);
        this._sendSubscribe(topic, subId);
      }
    });
  }

  subscribeChannel(channelId, planId) {
    this.setViewTopics('channels', { channelId, planId });
  }

  _startSSE() {
    if (this.eventSource) return;
    this.eventSource = new EventSource('/events');
    this.eventSource.onmessage = (ev) => {
      try {
        const payload = JSON.parse(ev.data);
        this.onMessage(payload);
      } catch {
        /* ignore non-JSON SSE */
      }
    };
  }

  _stopSSE() {
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }
  }

  disconnect() {
    clearTimeout(this._reconnectTimer);
    this._stopSSE();
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
    this._setMode('disconnected');
  }
}