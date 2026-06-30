import { useEffect, useMemo, useState } from 'react';
import { api } from '@/api/client';
import { EmptyState } from '@/components/ui/EmptyState';
import { formatPersonaLabel } from '@/lib/display';

type AgentCard = {
  name: string;
  status: string;
  task: string;
  progress: string;
  last_seen_seq?: number;
  cycles_since_turn?: number;
  last_outcome?: string;
  pending?: boolean;
  last_activity?: string;
  channel?: string;
  usage?: any;  // per-agent llm usage from by_agent
};

type Props = {
  onOpenTrace: (id: string) => void;
};

function isCourtPersona(name: string): boolean {
  return name.startsWith('court-persona-');
}

function isSpecialist(name: string): boolean {
  return (
    name.startsWith('project-manager') ||
    name.startsWith('coder') ||
    name.startsWith('tester') ||
    name.startsWith('agent') ||
    name.startsWith('builder')
  );
}

export function AgentsView({ onOpenTrace }: Props) {
  const [agents, setAgents] = useState<AgentCard[]>([]);
  const [usage, setUsage] = useState<any>(null);
  const [recent, setRecent] = useState<any>(null);

  useEffect(() => {
    api.agents().then((d) => setAgents(d.agents || [])).catch(() => {});
    api.llmUsage().then(setUsage).catch(() => {});
    // Fetch recent for time-series chart (small, client buckets)
    api.llmUsageRecent(150).then(setRecent).catch(() => {});
  }, []);

  // Attach per-agent usage to cards for display (from by_agent breakdown)
  const agentsWithUsage = useMemo(() => {
    if (!usage || !usage.by_agent) return agents;
    return agents.map(a => ({
      ...a,
      usage: usage.by_agent[a.name] || usage.by_agent[a.name.replace(/-main$/, '')] || null,
    }));
  }, [agents, usage]);

  // Dynamic time span + bucket size based on actual data availability (how long metrics have been collected / system running for LLM use).
  // Starts fine-grained when just starting (few records), grows to last ~24h with coarser buckets as more data arrives.
  const timeSeries = useMemo(() => {
    if (!recent?.records?.length) return [];
    const records = recent.records.filter((r: any) => r.timestamp);
    if (!records.length) return [];

    const now = Date.now();
    let minTs = Infinity;
    let maxTs = 0;
    for (const rec of records) {
      const t = new Date(rec.timestamp || rec.ts).getTime();
      if (t && t < minTs) minTs = t;
      if (t && t > maxTs) maxTs = t;
    }
    if (!isFinite(minTs)) minTs = now;
    if (maxTs === 0) maxTs = now;

    const dataSpanMs = Math.max(1, maxTs - minTs);
    // Cap display to last 24h worth, but use actual span if shorter (helps early users)
    const displaySpanMs = Math.min(24 * 3600 * 1000, dataSpanMs);

    // Choose bucket size based on span.
    // For ≥24h we use 1-hour buckets (still useful for overview).
    // Past ~72h we could consider 6h buckets, but for now we keep display
    // capped at 24h with 1h resolution.
    let bucketMs = 3600 * 1000; // 1h default for >= ~6h
    if (displaySpanMs < 3600 * 1000) {
      bucketMs = 5 * 60 * 1000; // 5 min
    } else if (displaySpanMs < 6 * 3600 * 1000) {
      bucketMs = 15 * 60 * 1000; // 15 min
    } else {
      bucketMs = 3600 * 1000; // 1h (covers 6h–24h and the >=24h cap)
    }

    const numBuckets = Math.max(2, Math.ceil(displaySpanMs / bucketMs));
    const buckets: any[] = [];
    const modelSet = new Set<string>();

    // Build buckets backward from maxTs (or now)
    let currentEnd = maxTs;
    for (let i = 0; i < numBuckets; i++) {
      const start = currentEnd - bucketMs;
      const end = currentEnd;
      const labelTime = new Date(end);
      buckets.unshift({
        start,
        end,
        label: labelTime.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
        byModel: {} as Record<string, number>,
      });
      currentEnd = start;
    }

    for (const rec of records) {
      const ts = new Date(rec.timestamp || rec.ts).getTime();
      if (!ts) continue;
      const tok = (rec.tokens_prompt || 0) + (rec.tokens_completion || 0);
      const model = rec.model || 'unknown';
      modelSet.add(model);
      for (const b of buckets) {
        if (ts >= b.start && ts < b.end) {
          b.byModel[model] = (b.byModel[model] || 0) + tok;
          break;
        }
      }
    }

    const models = Array.from(modelSet).sort();
    // Update labels to reflect actual span
    const hours = Math.round(displaySpanMs / 3600000);
    return buckets.map(b => ({ ...b, models, hours }));
  }, [recent]);

  // Precompute SVG elements for the stacked chart
  const chartElements = useMemo(() => {
    if (!timeSeries.length) return null;
    const w = 100;
    const h = 60;
    const bw = w / timeSeries.length;
    const maxTok = Math.max(1, ...timeSeries.map((b: any) => Object.values(b.byModel).reduce((s: number, v: any) => s + (v || 0), 0)));
    const colors: Record<string, string> = { qwen: '#4a9', llama: '#e67', gemma: '#9c6', unknown: '#888' };
    return timeSeries.map((b: any, i: number) => {
      const x = i * bw;
      let y = h;
      const els: any[] = [];
      Object.entries(b.byModel).sort().forEach(([m, tok]: [string, any], mi) => {
        const ch = Math.max(1, (tok / maxTok) * h);
        y -= ch;
        const base = m.split(':')[0].toLowerCase();
        const col = colors[base] || `hsl(${(mi * 70) % 360}, 55%, 52%)`;
        els.push(<rect key={m} x={x} y={y} width={bw - 1.5} height={ch} fill={col} opacity="0.82" />);
      });
      return (
        <g key={i}>
          {els}
          <title>{b.label}: {Math.round(Object.values(b.byModel).reduce((s: number, v: any) => s + (v || 0), 0))} tokens</title>
        </g>
      );
    });
  }, [timeSeries]);

  const { specialists, court } = useMemo(() => {
    const spec: any[] = [];
    const crt: any[] = [];
    for (const a of agentsWithUsage) {
      if (isCourtPersona(a.name)) {
        crt.push(a);
      } else if (isSpecialist(a.name)) {
        spec.push(a);
      } else {
        spec.push(a);
      }
    }
    spec.sort((a, b) => {
      const aPM = a.name.startsWith('project-manager') ? 0 : 1;
      const bPM = b.name.startsWith('project-manager') ? 0 : 1;
      return aPM - bPM || a.name.localeCompare(b.name);
    });
    return { specialists: spec, court: crt };
  }, [agentsWithUsage]);

  const renderAgent = (agent: AgentCard) => (
    <li
      key={agent.name}
      className="list-card"
      data-testid={`agent-card-${agent.name}`}
      onClick={() => onOpenTrace(agent.name)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => e.key === 'Enter' && onOpenTrace(agent.name)}
    >
      <strong>{formatPersonaLabel(agent.name)}</strong>
      <span className="subtle">
        {agent.status} • {agent.task}
        {agent.channel ? ` • ${agent.channel}` : ''}
      </span>
      {agent.usage && (
        <span className="subtle" style={{ fontSize: '0.75em', display: 'block', color: '#4a9' }}>
          usage: {agent.usage.calls || 0} calls, {(agent.usage.tokens_total || ((agent.usage.tokens_prompt||0)+(agent.usage.tokens_completion||0)))} tokens
          {agent.usage.by_model && Object.keys(agent.usage.by_model).length > 0 && ` • model: ${Object.keys(agent.usage.by_model)[0]}`}
        </span>
      )}
      {(agent.last_seen_seq != null || agent.cycles_since_turn != null || agent.last_outcome) && (
        <span className="subtle" style={{ fontSize: '0.8em', display: 'block' }}>
          turn: seen={agent.last_seen_seq ?? '-'} cycles={agent.cycles_since_turn ?? '-'} {agent.last_outcome ? `outcome=${agent.last_outcome}` : ''} {agent.pending ? '(pending)' : ''}
        </span>
      )}
    </li>
  );

  return (
    <section className="panel content-panel" data-testid="agents-panel" data-page="agents">
      <header>
        <p className="eyebrow">Fleet</p>
        <h1>Agents</h1>
      </header>
      {usage && (
        <div data-testid="metrics-summary" style={{ marginBottom: '1rem', fontSize: '0.9em' }}>
          <strong>LLM usage:</strong> grand {usage.grand?.calls || 0} calls, {usage.grand?.tokens_total || 0} tokens
          {' '}(last hour {usage.last_hour?.calls || 0})
        </div>
      )}

      {timeSeries.length > 0 && chartElements && (
        <div style={{ margin: '0 0 8px', fontSize: '0.75em' }} data-testid="llm-timeseries">
          <div style={{ marginBottom: 2, color: '#666', fontSize: '9px' }}>
            tokens per model (last ~{timeSeries[0]?.hours || '?'}h, 1h buckets when ≥6h, stacked)
          </div>
          <svg width="100%" height="58" style={{ display: 'block' }} viewBox="0 0 100 58">
            {chartElements}
          </svg>
          <div style={{ fontSize: '8px', color: '#888', display: 'flex', justifyContent: 'space-between' }}>
            <span>{timeSeries[0]?.label}</span>
            <span>{timeSeries[timeSeries.length-1]?.label}</span>
          </div>
        </div>
      )}
      {agents.length === 0 ? (
        <EmptyState
          testId="agents-empty-state"
          eyebrow="Fleet"
          title="No agents running"
          description="Active microVM agents appear here when the harness spins up specialists for channel work."
          hint="Submit a goal from Home or post in a channel to wake the Project Manager and roster."
        />
      ) : (
        <>
          <section data-testid="agents-specialists-section">
            <h2 className="subtle">Specialists</h2>
            <ul className="list-stack" data-testid="agents-specialists-list">
              {specialists.length === 0 ? (
                <li className="subtle">No specialist agents running.</li>
              ) : (
                specialists.map(renderAgent)
              )}
            </ul>
          </section>
          {court.length > 0 && (
            <section data-testid="agents-court-section">
              <h2 className="subtle">Court personas</h2>
              <ul className="list-stack" data-testid="agents-court-list">
                {court.map(renderAgent)}
              </ul>
            </section>
          )}
        </>
      )}
    </section>
  );
}
