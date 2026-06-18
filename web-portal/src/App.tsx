import { useCallback, useEffect, useMemo } from 'react';
import { PortalView } from '@/contracts';
import { useRealtime } from '@/hooks/useRealtime';
import { useIsMobile } from '@/hooks/useMediaQuery';
import { usePortalStore } from '@/store/portalStore';
import { TopBar } from '@/components/layout/TopBar';
import { BottomNav } from '@/components/layout/BottomNav';
import { Sidebar } from '@/components/layout/Sidebar';
import { ContextPanel } from '@/components/layout/ContextPanel';
import { BottomSheet } from '@/components/layout/BottomSheet';
import { HomeView } from '@/views/HomeView';
import { ChannelsView } from '@/views/ChannelsView';
import { DashboardView } from '@/views/DashboardView';
import { CourtView } from '@/views/CourtView';
import { CanvasView } from '@/views/CanvasView';
import { AgentsView } from '@/views/AgentsView';
import { TraceView } from '@/views/TraceView';
import { SkillsView } from '@/views/SkillsView';
import { SettingsView } from '@/views/SettingsView';
import { AuditView } from '@/views/AuditView';
import { TeamsView } from '@/views/TeamsView';

const LAYOUT_BY_PAGE: Partial<Record<PortalView, string>> = {
  home: 'layout--home',
  channels: 'layout--channels',
  dashboard: 'layout--dashboard',
  monitoring: 'layout--dashboard',
  canvas: 'layout--canvas',
  trace: 'layout--trace',
};

function parseHash(): PortalView {
  const raw = (location.hash || '#home').replace(/^#/, '').split('?')[0];
  const valid: PortalView[] = [
    'home', 'channels', 'dashboard', 'court', 'canvas', 'agents', 'skills', 'audit', 'settings', 'trace', 'monitoring', 'teams',
  ];
  return valid.includes(raw as PortalView) ? (raw as PortalView) : 'home';
}

function parseTraceAgent(): string | null {
  const q = location.hash.split('?')[1];
  if (!q) return null;
  return new URLSearchParams(q).get('agent');
}

export default function App() {
  const isMobile = useIsMobile();
  const ready = usePortalStore((s) => s.ready);
  const view = usePortalStore((s) => s.view);
  const channels = usePortalStore((s) => s.channels);
  const currentChannel = usePortalStore((s) => s.currentChannel);
  const harnessByChannel = usePortalStore((s) => s.harnessByChannel);
  const contextPanelOpen = usePortalStore((s) => s.contextPanelOpen);
  const bottomSheetOpen = usePortalStore((s) => s.bottomSheetOpen);
  const setView = usePortalStore((s) => s.setView);
  const setBottomSheetOpen = usePortalStore((s) => s.setBottomSheetOpen);
  const setContextPanelOpen = usePortalStore((s) => s.setContextPanelOpen);
  const setTraceAgent = usePortalStore((s) => s.setTraceAgent);
  const loadInitial = usePortalStore((s) => s.loadInitial);
  const selectChannel = usePortalStore((s) => s.selectChannel);

  const realtimeCtx = useMemo(
    () => ({
      channelId: currentChannel?.id,
      planId: currentChannel ? harnessByChannel[currentChannel.id]?.plan?.plan_id : undefined,
      sessionId: usePortalStore.getState().traceAgentId || undefined,
      proposalId: usePortalStore.getState().selectedProposal?.id,
    }),
    [currentChannel?.id, harnessByChannel, currentChannel],
  );

  useRealtime(view, realtimeCtx);

  useEffect(() => {
    loadInitial().catch(() => usePortalStore.setState({ ready: true }));
  }, [loadInitial]);

  // Auto-select first channel when entering Channels view once channel list is loaded.
  useEffect(() => {
    if (view === 'channels' && !currentChannel && channels.length > 0) {
      selectChannel(channels[0]);
    }
  }, [view, currentChannel, channels, selectChannel]);

  useEffect(() => {
    const sync = () => {
      const page = parseHash();
      setView(page === 'monitoring' ? 'dashboard' : page);
      const agent = parseTraceAgent();
      if (agent) setTraceAgent(agent);
      if (page === 'channels' && !usePortalStore.getState().currentChannel) {
        const first = usePortalStore.getState().channels[0];
        if (first) selectChannel(first);
      }
    };
    sync();
    window.addEventListener('hashchange', sync);
    return () => window.removeEventListener('hashchange', sync);
  }, [setView, setTraceAgent, selectChannel]);

  const navigate = useCallback((page: PortalView) => {
    location.hash = page === 'dashboard' ? 'dashboard' : page;
    setView(page);
  }, [setView]);

  const openChannel = useCallback(
    async (channelId: string) => {
      const ch = channels.find((c) => c.id === channelId) || { id: channelId, members: [] };
      await selectChannel(ch);
      navigate('channels');
    },
    [channels, selectChannel, navigate],
  );

  const openCanvas = useCallback(() => navigate('canvas'), [navigate]);
  const openTrace = useCallback(
    (id: string) => {
      setTraceAgent(id);
      location.hash = `trace?agent=${encodeURIComponent(id)}`;
      setView('trace');
    },
    [setTraceAgent, setView],
  );
  const openCourt = useCallback(
    (proposalId: string) => {
      const p = usePortalStore.getState().proposals.find((x) => x.id === proposalId);
      if (p) usePortalStore.getState().setSelectedProposal(p);
      navigate('court');
    },
    [navigate],
  );

  const layoutClass = LAYOUT_BY_PAGE[view] || 'layout--simple';
  const showSidebar = !['canvas', 'trace', 'teams'].includes(view);
  const showContext = ['home', 'channels', 'dashboard'].includes(view) && !isMobile && contextPanelOpen;
  const channelHarness = currentChannel ? harnessByChannel[currentChannel.id] : harnessByChannel.main;

  const renderPage = () => {
    switch (view) {
      case 'home':
        return <HomeView onOpenChannel={openChannel} onOpenCanvas={openCanvas} />;
      case 'channels':
        return (
          <ChannelsView
            onOpenCanvas={openCanvas}
            onOpenContext={() => setBottomSheetOpen(true)}
          />
        );
      case 'dashboard':
      case 'monitoring':
        return <DashboardView onOpenCanvas={openCanvas} onOpenTrace={openTrace} onOpenCourt={openCourt} />;
      case 'court':
        return <CourtView />;
      case 'canvas':
        return <CanvasView onOpenChannel={openChannel} />;
      case 'agents':
        return <AgentsView onOpenTrace={openTrace} />;
      case 'trace':
        return <TraceView />;
      case 'skills':
        return <SkillsView />;
      case 'settings':
        return <SettingsView />;
      case 'audit':
        return <AuditView />;
      case 'teams':
        return <TeamsView />;
      default:
        return <HomeView onOpenChannel={openChannel} onOpenCanvas={openCanvas} />;
    }
  };

  return (
    <div className="app-shell" data-testid="app-shell" data-portal-ready={ready ? '1' : '0'}>
      <TopBar onNavigate={navigate} />
      <main className={`workspace-grid ${layoutClass}`} data-testid="workspace-grid">
        {showSidebar && (
          <Sidebar
            channels={channels}
            currentChannelId={currentChannel?.id}
            onSelect={(ch) => {
              selectChannel(ch);
              navigate('channels');
            }}
            onNavigate={navigate}
          />
        )}
        <section className="main-column">{renderPage()}</section>
        {showContext && (
          <ContextPanel
            harness={channelHarness}
            channelId={currentChannel?.id}
            collapsed={!contextPanelOpen}
          />
        )}
        {showContext && (
          <button
            type="button"
            className="secondary-button context-toggle"
            onClick={() => setContextPanelOpen(!contextPanelOpen)}
            aria-label="Toggle context panel"
          >
            {contextPanelOpen ? '⟩' : '⟨'}
          </button>
        )}
      </main>
      <BottomNav view={view} onNavigate={navigate} />
      {isMobile && (
        <BottomSheet
          open={bottomSheetOpen}
          title="Channel Context"
          onClose={() => setBottomSheetOpen(false)}
        >
          <ContextPanel harness={channelHarness} channelId={currentChannel?.id} />
        </BottomSheet>
      )}
    </div>
  );
}