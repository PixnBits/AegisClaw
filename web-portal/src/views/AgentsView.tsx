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

  useEffect(() => {
    api.agents().then((d) => setAgents(d.agents || [])).catch(() => {});
  }, []);

  const { specialists, court } = useMemo(() => {
    const spec: AgentCard[] = [];
    const crt: AgentCard[] = [];
    for (const a of agents) {
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
  }, [agents]);

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
