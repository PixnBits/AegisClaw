import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { EmptyState } from '@/components/ui/EmptyState';

type Props = {
  onOpenTrace: (id: string) => void;
};

export function AgentsView({ onOpenTrace }: Props) {
  const [agents, setAgents] = useState<Array<{ name: string; status: string; task: string; progress: string }>>([]);

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
            </span>
          </li>
        ))
        )}
      </ul>
    </section>
  );
}