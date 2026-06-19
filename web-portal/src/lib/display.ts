/** Human-friendly label for agent roles and personas */
const ACRONYMS = new Set(['ciso', 'sdlc', 'pm', 'api', 'ux']);

export function formatPersonaLabel(role: string): string {
  if (!role) return 'Member';
  if (role.startsWith('user:')) return role.slice(5);
  if (role === 'user') return 'You';
  const stripped = role
    .replace(/^court-persona-/, '')
    .replace(/^persona-/, '')
    .replace(/-/g, ' ');
  return stripped
    .split(' ')
    .filter(Boolean)
    .map((w) => (ACRONYMS.has(w.toLowerCase()) ? w.toUpperCase() : w.charAt(0).toUpperCase() + w.slice(1)))
    .join(' ');
}
