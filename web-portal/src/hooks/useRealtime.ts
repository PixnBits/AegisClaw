import { useEffect, useRef } from 'react';
import { RealtimeClient } from '@/realtime/stompClient';
import { usePortalStore } from '@/store/portalStore';
import { ViewContext } from '@/contracts';

export function useRealtime(view: string, ctx: ViewContext = {}) {
  const clientRef = useRef<RealtimeClient | null>(null);
  const handleRealtime = usePortalStore((s) => s.handleRealtime);
  const setConnectionMode = usePortalStore((s) => s.setConnectionMode);

  useEffect(() => {
    const client = new RealtimeClient({
      onMessage: handleRealtime,
      onStatus: setConnectionMode,
    });
    clientRef.current = client;
    client.connect();
    return () => client.disconnect();
  }, [handleRealtime, setConnectionMode]);

  useEffect(() => {
    clientRef.current?.setViewTopics(view as Parameters<RealtimeClient['setViewTopics']>[0], ctx);
  }, [view, ctx.channelId, ctx.channelIds?.join(','), ctx.planId, ctx.planIds?.join(','), ctx.sessionId, ctx.proposalId]);
}