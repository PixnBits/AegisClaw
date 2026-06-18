import { useEffect, useState } from 'react';
import { api } from '@/api/client';

export function SkillsView() {
  const [skills, setSkills] = useState<Array<Record<string, unknown>>>([]);

  useEffect(() => {
    api.skills().then((d) => setSkills(Array.isArray(d) ? (d as Record<string, unknown>[]) : [])).catch(() => {});
  }, []);

  return (
    <section className="panel content-panel" data-testid="skills-panel" data-page="skills">
      <header>
        <p className="eyebrow">Registry</p>
        <h1>Skills</h1>
        <button type="button" className="primary-button" data-testid="propose-skill-button">
          Propose Skill
        </button>
      </header>
      <div className="card-grid" data-testid="skills-list">
        {skills.map((skill) => (
          <article key={String(skill.id)} className="list-card">
            <strong>{String(skill.name || skill.id)}</strong>
            <p className="subtle">{String(skill.description || '')}</p>
            <span className="badge badge--pending">{String(skill.status || 'registered')}</span>
          </article>
        ))}
      </div>
    </section>
  );
}