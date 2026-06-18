import { create } from 'zustand';
import { api } from '@/api/client';
import { applyHarnessEvent } from '@/lib/harness';
import { messageToFeedItem } from '@/lib/reasoning';
import {
  Channel,
  DashboardData,
  EVENT,
  FeedItem,
  HarnessState,
  OverviewStats,
  PortalView,
  Proposal,
} from '@/contracts';

export type PlanPreview = {
  plan_id: string;
  channel_id: string;
  goal: string;
  stages: HarnessState['plan'] extends infer P ? P extends { stages: infer S } ? S : never : never;
};

type PortalState = {
  ready: boolean;
  view: PortalView;
  connectionMode: 'disconnected' | 'stomp' | 'sse-fallback';
  dashboard: DashboardData | null;
  channels: Channel[];
  currentChannel: Channel | null;
  harnessByChannel: Record<string, HarnessState>;
  feedByChannel: Record<string, FeedItem[]>;
  proposals: Proposal[];
  selectedProposal: Proposal | null;
  planPreview: PlanPreview | null;
  traceAgentId: string | null;
  overviewStats: OverviewStats | null;
  contextPanelOpen: boolean;
  bottomSheetOpen: boolean;
  /** E2E / test hook — prevents auto-selecting the first channel */
  skipChannelAutoSelect: boolean;
  expandedReasoning: Set<string>;
  dashboardFilter: string;

  setView: (view: PortalView) => void;
  setReady: (ready: boolean) => void;
  setConnectionMode: (mode: PortalState['connectionMode']) => void;
  setContextPanelOpen: (open: boolean) => void;
  setBottomSheetOpen: (open: boolean) => void;
  toggleReasoningExpanded: (id: string) => void;
  setDashboardFilter: (filter: string) => void;

  loadInitial: () => Promise<void>;
  loadChannels: () => Promise<void>;
  selectChannel: (ch: Channel) => Promise<void>;
  loadHarness: (channelId: string) => Promise<HarnessState | null>;
  submitGoal: (goal: string) => Promise<PlanPreview>;
  clearPlanPreview: () => void;
  postMessage: (content: string) => Promise<void>;
  handleRealtime: (payload: Record<string, unknown>) => void;
  refreshChannelMessages: () => Promise<void>;
  setSelectedProposal: (p: Proposal | null) => void;
  setTraceAgent: (id: string | null) => void;
  collapseAllFeedReasoning: (channelId: string) => void;
};

export const usePortalStore = create<PortalState>((set, get) => ({
  ready: false,
  view: 'home',
  connectionMode: 'disconnected',
  dashboard: null,
  channels: [],
  currentChannel: null,
  harnessByChannel: {},
  feedByChannel: {},
  proposals: [],
  selectedProposal: null,
  planPreview: null,
  traceAgentId: null,
  overviewStats: null,
  contextPanelOpen: true,
  bottomSheetOpen: false,
  skipChannelAutoSelect: false,
  expandedReasoning: new Set(),
  dashboardFilter: 'all',

  setView: (view) => set({ view }),
  setReady: (ready) => set({ ready }),
  setConnectionMode: (connectionMode) => set({ connectionMode }),
  setContextPanelOpen: (contextPanelOpen) => set({ contextPanelOpen }),
  setBottomSheetOpen: (bottomSheetOpen) => set({ bottomSheetOpen }),
  toggleReasoningExpanded: (id) =>
    set((s) => {
      const next = new Set(s.expandedReasoning);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return { expandedReasoning: next };
    }),
  setDashboardFilter: (dashboardFilter) => set({ dashboardFilter }),
  setSelectedProposal: (selectedProposal) => set({ selectedProposal }),
  setTraceAgent: (traceAgentId) => set({ traceAgentId }),

  loadInitial: async () => {
    const [dash, proposals] = await Promise.allSettled([
      api.dashboard(),
      api.proposals(),
    ]);
    if (dash.status === 'fulfilled') set({ dashboard: dash.value });
    if (proposals.status === 'fulfilled') set({ proposals: proposals.value });
    await get().loadChannels();
    await get().loadHarness('main');
    set({ ready: true });
  },

  loadChannels: async () => {
    const data = await api.channels();
    set({ channels: (data.channels || []).filter((c) => !c.archived) });
  },

  selectChannel: async (ch) => {
    set({ currentChannel: ch, view: 'channels' });
    try {
      const full = await api.channel(ch.id);
      const messages = full.messages || [];
      const feed = messages.map((m, i) => messageToFeedItem(m, ch.id, i));
      set((s) => ({
        currentChannel: full,
        feedByChannel: { ...s.feedByChannel, [ch.id]: feed },
      }));
      await get().loadHarness(ch.id);
    } catch {
      const feed = (ch.messages || []).map((m, i) => messageToFeedItem(m, ch.id, i));
      set((s) => ({
        feedByChannel: { ...s.feedByChannel, [ch.id]: feed },
      }));
      await get().loadHarness(ch.id);
    }
  },

  loadHarness: async (channelId) => {
    try {
      const data = await api.harness(channelId);
      set((s) => ({
        harnessByChannel: { ...s.harnessByChannel, [channelId]: data },
      }));
      return data;
    } catch {
      return null;
    }
  },

  submitGoal: async (goal) => {
    const preview = await api.goals(goal);
    set((s) => ({
      planPreview: preview,
      harnessByChannel: {
        ...s.harnessByChannel,
        [preview.channel_id]: {
          plan: {
            plan_id: preview.plan_id,
            channel_id: preview.channel_id,
            goal: preview.goal,
            status: 'active',
            stages: preview.stages as HarnessState['plan'] extends infer P
              ? P extends { stages: infer St } ? St : never
              : never,
          },
          tasks: [],
        },
      },
    }));
    return preview;
  },

  clearPlanPreview: () => set({ planPreview: null }),

  postMessage: async (content) => {
    const ch = get().currentChannel;
    if (!ch) return;
    await api.postChannel(ch.id, content);
    await get().refreshChannelMessages();
  },

  refreshChannelMessages: async () => {
    const ch = get().currentChannel;
    if (!ch) return;
    const full = await api.channel(ch.id);
    const feed = (full.messages || []).map((m, i) => messageToFeedItem(m, ch.id, i));
    set((s) => ({
      currentChannel: full,
      feedByChannel: { ...s.feedByChannel, [ch.id]: feed },
    }));
  },

  handleRealtime: (payload) => {
    const type = String(payload.type || '');
    if (type === EVENT.overviewStats) {
      set({ overviewStats: payload as unknown as OverviewStats });
      return;
    }
    if (type === EVENT.channelActivity && payload.channel_id) {
      const channelId = String(payload.channel_id);
      let inner: Record<string, unknown> | null = null;
      if (typeof payload.event === 'string') {
        try {
          inner = JSON.parse(payload.event);
        } catch {
          inner = null;
        }
      } else if (payload.event && typeof payload.event === 'object') {
        inner = payload.event as Record<string, unknown>;
      }
      if (inner?.type && String(inner.type).startsWith('harness.')) {
        set((s) => {
          const prev = s.harnessByChannel[channelId] || { plan: null, tasks: [] };
          return {
            harnessByChannel: {
              ...s.harnessByChannel,
              [channelId]: applyHarnessEvent(prev, inner!),
            },
          };
        });
        return;
      }
      if (get().currentChannel?.id === channelId) {
        get().refreshChannelMessages();
      }
      return;
    }
    if (type.startsWith('harness.')) {
      const channelId = String(payload.channel_id || get().currentChannel?.id || 'main');
      set((s) => {
        const prev = s.harnessByChannel[channelId] || { plan: null, tasks: [] };
        return {
          harnessByChannel: {
            ...s.harnessByChannel,
            [channelId]: applyHarnessEvent(prev, payload),
          },
        };
      });
    }
  },

  collapseAllFeedReasoning: (channelId) => {
    set((s) => {
      const feed = s.feedByChannel[channelId] || [];
      return {
        feedByChannel: {
          ...s.feedByChannel,
          [channelId]: feed.map((item) =>
            item.kind === 'agent_reasoning' || item.kind === 'tool_call'
              ? { ...item, inFlight: false, decisive: true, collapsedSummary: item.collapsedSummary }
              : item,
          ),
        },
      };
    });
  },
}));

declare global {
  interface Window {
    __portalStore?: typeof usePortalStore;
  }
}

if (typeof window !== 'undefined') {
  window.__portalStore = usePortalStore;
}