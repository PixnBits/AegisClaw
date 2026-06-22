import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { EmptyState } from '@/components/ui/EmptyState';

type Props = {
  onOpenTrace: (id: string) => void;
};

export function AgentsView({ onOpenTrace }: Props) {
  const [agents, setAgents] = useState<Array<{ name: string; status: string; task: string; progress: string; last_seen_seq?: number; cycles_since_turn?: number; last_outcome?: string; pending?: boolean; last_activity?: string; channel?: string }>>([]);

  useEffect(() => {
    api.agents().then((d) => setAgents(d.agents || [])).catch(() => {});
  }, []);

  return (
    <section className="panel content-panel" data-testid="agents-panel" data-page="agents">
      <header>
        <p className="eyebrow">Fleet</p>
        <h1>Agents</h1>
      </header>
      <ul className="list-stack" data-testid="agents-list">
        {agents.length === 0 ? (
          <EmptyState
            testId="agents-empty-state"
            eyebrow="Fleet"
            title="No agents running"
            description="Active microVM agents appear here when the harness spins up specialists for channel work."
            hint="Submit a goal from Home or post in a channel to wake the Project Manager and roster."
          />
        ) : (
          agents.map((agent) => (
          <li
            key={agent.name}
            className="list-card"
            onClick={() => onOpenTrace(agent.name)}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => e.key === 'Enter' && onOpenTrace(agent.name)}
          >
            <strong>{agent.name}</strong>
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
        ))
        )}
      </ul>
    </section>
  );
}