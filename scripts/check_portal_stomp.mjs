#!/usr/bin/env node
/**
 * STOMP over WebSocket contract check for portal channel updates.
 * Connects to /stomp, subscribes to a channel topic, posts via REST, expects MESSAGE.
 */
import { setTimeout as delay } from 'node:timers/promises';

const portalURL = process.env.AEGIS_PORTAL_URL || 'http://localhost:8080';
const channelId = process.env.AEGIS_STOMP_CHANNEL || 'main';
const timeoutMs = Number(process.env.AEGIS_STOMP_TIMEOUT_MS || 20000);

const wsURL = portalURL.replace(/^http/, 'ws') + '/stomp';
const marker = `STOMP-E2E-${Date.now()}`;

function parseStompFrames(buffer) {
  const frames = [];
  let rest = buffer;
  while (rest.includes('\x00')) {
    const idx = rest.indexOf('\x00');
    const raw = rest.slice(0, idx);
    rest = rest.slice(idx + 1);
    if (!raw.trim()) continue;
    const [head, body = ''] = raw.split('\n\n', 2);
    const lines = head.split('\n');
    const command = lines[0]?.trim() || '';
    const headers = {};
    for (const line of lines.slice(1)) {
      const i = line.indexOf(':');
      if (i > 0) headers[line.slice(0, i).trim()] = line.slice(i + 1).trim();
    }
    frames.push({ command, headers, body });
  }
  return { frames, rest };
}

async function main() {
  const deadline = Date.now() + timeoutMs;
  let connected = false;
  let subscribed = false;
  let buffer = '';

  const ws = new WebSocket(wsURL);

  const postPromise = (async () => {
    while (!subscribed && Date.now() < deadline) {
      await delay(50);
    }
    if (!subscribed) throw new Error('never subscribed to channel topic');
    const res = await fetch(`${portalURL}/api/channels/${channelId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ from: 'user', content: `${marker}: stomp notify` }),
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`channel post HTTP ${res.status}: ${text.slice(0, 200)}`);
    }
  })();

  const messagePromise = new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(`STOMP MESSAGE not received within ${timeoutMs}ms`));
    }, timeoutMs);

    ws.addEventListener('open', () => {
      ws.send('CONNECT\naccept-version:1.2\n\n\x00');
    });

    ws.addEventListener('message', (ev) => {
      buffer += String(ev.data);
      const parsed = parseStompFrames(buffer);
      buffer = parsed.rest;
      for (const frame of parsed.frames) {
        if (frame.command === 'CONNECTED') {
          connected = true;
          ws.send(
            `SUBSCRIBE\nid:sub-${channelId}\ndestination:/topic/channels.${channelId}.messages\n\n\x00`,
          );
          subscribed = true;
          continue;
        }
        if (frame.command === 'MESSAGE') {
          const payload = frame.body || '';
          if (payload.includes(marker)) {
            clearTimeout(timer);
            resolve(frame);
          }
        }
        if (frame.command === 'ERROR') {
          clearTimeout(timer);
          reject(new Error(`STOMP ERROR: ${frame.body || frame.headers.message || 'unknown'}`));
        }
      }
    });

    ws.addEventListener('error', (err) => {
      clearTimeout(timer);
      reject(err);
    });

    ws.addEventListener('close', () => {
      if (!connected) {
        clearTimeout(timer);
        reject(new Error('WebSocket closed before STOMP CONNECTED'));
      }
    });
  });

  try {
    await postPromise;
    const frame = await messagePromise;
    console.log(`✓ STOMP MESSAGE on /topic/channels.${channelId}.messages (${frame.body.length} bytes)`);
    ws.close();
    process.exit(0);
  } catch (err) {
    console.error(`✗ STOMP check failed: ${err.message || err}`);
    try {
      ws.close();
    } catch {
      /* ignore */
    }
    process.exit(1);
  }
}

main();
