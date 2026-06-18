import { create } from 'zustand';
import { ReasoningPolicy } from '@/contracts';

type PolicyState = {
  globalPolicy: ReasoningPolicy;
  channelPolicies: Record<string, ReasoningPolicy>;
  enterpriseLocked: boolean;
  setGlobalPolicy: (policy: ReasoningPolicy) => void;
  setChannelPolicy: (channelId: string, policy: ReasoningPolicy) => void;
  effectivePolicy: (channelId?: string) => ReasoningPolicy;
};

export const usePolicyStore = create<PolicyState>((set, get) => ({
  globalPolicy: 'progressive',
  channelPolicies: {},
  enterpriseLocked: false,

  setGlobalPolicy: (globalPolicy) => {
    if (get().enterpriseLocked) return;
    set({ globalPolicy });
  },

  setChannelPolicy: (channelId, policy) => {
    if (get().enterpriseLocked) return;
    set((s) => ({
      channelPolicies: { ...s.channelPolicies, [channelId]: policy },
    }));
  },

  effectivePolicy: (channelId) => {
    const s = get();
    if (channelId && s.channelPolicies[channelId]) return s.channelPolicies[channelId];
    return s.globalPolicy;
  },
}));