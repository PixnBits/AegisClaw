import { FormEvent, useEffect, useState } from 'react';

type Team = { id: string; name?: string; roles?: string[]; messages?: number };

export function TeamsView() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [teamId, setTeamId] = useState('');
  const [roles, setRoles] = useState('researcher,coder');

  useEffect(() => {
    fetch('/api/teams', { credentials: 'same-origin' })
      .then((r) => r.json())
      .then((d) => setTeams(Array.isArray(d?.teams) ? d.teams : Array.isArray(d) ? d : []))
      .catch(() => setTeams([]));
  }, []);

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault();
    if (!teamId.trim()) return;
    await fetch('/api/teams/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ id: teamId.trim(), roles: roles.split(',').map((r) => r.trim()) }),
    });
    setTeamId('');
    const res = await fetch('/api/teams', { credentials: 'same-origin' });
    const d = await res.json();
    setTeams(Array.isArray(d?.teams) ? d.teams : []);
  };

  return (
    <section className="panel content-panel" data-testid="teams-panel" data-page="teams">
      <header>
        <p className="eyebrow">Collaboration</p>
        <h1>Team Workspace</h1>
      </header>
      <form id="create-team-form" className="inline-form" data-testid="create-team-form" onSubmit={handleCreate} noValidate>
        <input type="text" placeholder="team-id" value={teamId} onChange={(e) => setTeamId(e.target.value)} required />
        <input type="text" placeholder="roles" value={roles} onChange={(e) => setRoles(e.target.value)} />
        <button type="submit" className="primary-button">
          Create Team
        </button>
      </form>
      <ul id="teamsList" className="list-stack" data-testid="teams-list">
        {teams.map((t) => (
          <li key={t.id} className="list-card">
            <strong>{t.name || t.id}</strong>
            {t.roles?.length ? <small className="subtle">{t.roles.join(', ')}</small> : null}
          </li>
        ))}
      </ul>
    </section>
  );
}