import { create } from 'zustand';
import { api } from '@/api/client';
import { appendFeedItem, feedItemFromActivityPayload } from '@/lib/channelActivity';
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
  MonitoringStats,
  SecurityPosture,
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
  monitoringStats: MonitoringStats | null;
  securityPosture: SecurityPosture | null;
  contextPanelOpen: boolean;
  bottomSheetOpen: boolean;
  moreMenuOpen: boolean;
  /** E2E / test hook — prevents auto-selecting the first channel */
  skipChannelAutoSelect: boolean;
  expandedReasoning: Set<string>;
  dashboardFilter: string;
  /** Unread message bursts per channel (debounced for active agent traffic) */
  unreadByChannel: Record<string, number>;

  setView: (view: PortalView) => void;
  setReady: (ready: boolean) => void;
  setConnectionMode: (mode: PortalState['connectionMode']) => void;
  setContextPanelOpen: (open: boolean) => void;
  setBottomSheetOpen: (open: boolean) => void;
  setMoreMenuOpen: (open: boolean) => void;
  toggleReasoningExpanded: (id: string) => void;
  setDashboardFilter: (filter: string) => void;

  loadInitial: () => Promise<void>;
  loadSecurityPosture: () => Promise<void>;
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
  bumpUnread: (channelId: string) => void;
  clearUnread: (channelId: string) => void;
};

const UNREAD_DEBOUNCE_MS = 4000;
const unreadBurstActive: Record<string, boolean> = {};

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
  monitoringStats: null,
  securityPosture: null,
  contextPanelOpen: true,
  bottomSheetOpen: false,
  moreMenuOpen: false,
  skipChannelAutoSelect: false,
  expandedReasoning: new Set(),
  dashboardFilter: 'all',
  unreadByChannel: {},

  setView: (view) => set({ view }),
  setReady: (ready) => set({ ready }),
  setConnectionMode: (connectionMode) => set({ connectionMode }),
  setContextPanelOpen: (contextPanelOpen) => set({ contextPanelOpen }),
  setBottomSheetOpen: (bottomSheetOpen) => set({ bottomSheetOpen }),
  setMoreMenuOpen: (moreMenuOpen) => set({ moreMenuOpen }),
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
    const [dash, proposals, posture] = await Promise.allSettled([
      api.dashboard(),
      api.proposals(),
      api.securityPosture(),
    ]);
    if (dash.status === 'fulfilled') set({ dashboard: dash.value });
    if (proposals.status === 'fulfilled') set({ proposals: proposals.value });
    if (posture.status === 'fulfilled') set({ securityPosture: posture.value });
    await get().loadChannels();
    await get().loadHarness('main');
    set({ ready: true });
  },

  loadSecurityPosture: async () => {
    try {
      const posture = await api.securityPosture();
      set({ securityPosture: posture });
    } catch {
      /* optional surface */
    }
  },

  loadChannels: async () => {
    const data = await api.channels();
    set({ channels: (data.channels || []).filter((c) => !c.archived) });
  },

  selectChannel: async (ch) => {
    get().clearUnread(ch.id);
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
    // STOMP from portal POST usually arrives first; refresh is fallback only.
    await get().refreshChannelMessages();
    // Agent replies are async; poll briefly when daemon→guest STOMP notify is unavailable.
    const channelId = ch.id;
    for (const delayMs of [4000, 12000]) {
      setTimeout(() => {
        if (get().currentChannel?.id === channelId) {
          void get().refreshChannelMessages();
        }
      }, delayMs);
    }
  },

  bumpUnread: (channelId) => {
    const { currentChannel, view } = get();
    if (view === 'channels' && currentChannel?.id === channelId) return;

    if (!unreadBurstActive[channelId]) {
      unreadBurstActive[channelId] = true;
      set((s) => ({
        unreadByChannel: {
          ...s.unreadByChannel,
          [channelId]: (s.unreadByChannel[channelId] || 0) + 1,
        },
      }));
      setTimeout(() => {
        delete unreadBurstActive[channelId];
      }, UNREAD_DEBOUNCE_MS);
    }
  },

  clearUnread: (channelId) => {
    delete unreadBurstActive[channelId];
    set((s) => {
      if (!s.unreadByChannel[channelId]) return s;
      const next = { ...s.unreadByChannel };
      delete next[channelId];
      return { unreadByChannel: next };
    });
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
    if (type === EVENT.monitoringStats) {
      set({ monitoringStats: payload as unknown as MonitoringStats });
      return;
    }
    if (type === EVENT.canvasEvent) {
      const channelId = String(payload.channel_id || get().currentChannel?.id || 'main');
      set((s) => {
        const prev = s.harnessByChannel[channelId] || { plan: null, tasks: [] };
        const taskId = String(payload.task_id || payload.persona_task_id || '');
        if (!taskId) return s;
        const tasks = (prev.tasks || []).map((t) =>
          t.task_id === taskId
            ? {
                ...t,
                progress: Number(payload.progress ?? t.progress),
                current_stage: String(payload.stage || t.current_stage),
                agent_persona: String(payload.persona || t.agent_persona),
              }
            : t,
        );
        return { harnessByChannel: { ...s.harnessByChannel, [channelId]: { ...prev, tasks } } };
      });
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

      const feedItem = feedItemFromActivityPayload(payload, channelId);
      if (feedItem) {
        const isActive =
          get().view === 'channels' && get().currentChannel?.id === channelId;
        if (isActive) {
          set((s) => ({
            feedByChannel: {
              ...s.feedByChannel,
              [channelId]: appendFeedItem(s.feedByChannel[channelId] || [], feedItem),
            },
          }));
        } else {
          get().bumpUnread(channelId);
        }
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