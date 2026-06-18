export type Member = { role?: string; agent_id?: string };

export type MemberGroup = 'Core Court' | 'Project / SDLC' | 'Humans';

export function memberRole(m: Member): string {
  return m.role || m.agent_id || 'member';
}

export function groupMembers(members: Member[]): Record<MemberGroup, Member[]> {
  const groups: Record<MemberGroup, Member[]> = {
    'Core Court': [],
    'Project / SDLC': [],
    Humans: [],
  };
  (members || []).forEach((m) => {
    const role = memberRole(m);
    if (role.startsWith('court-persona-')) groups['Core Court'].push(m);
    else if (role.startsWith('user:') || role === 'user') groups.Humans.push(m);
    else groups['Project / SDLC'].push(m);
  });
  return groups;
}