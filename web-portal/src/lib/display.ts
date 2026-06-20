/** Human-friendly label for agent roles and personas */
const ACRONYMS = new Set(['ciso', 'sdlc', 'pm', 'api', 'ux']);

const MEMORY_GB_THRESHOLD_MB = 1280; // 1.25 GB

/** Format host RAM label; converts to GB when total exceeds ~1.25 GB */
export function formatMemoryUsage(label: string | undefined | null): string {
  if (!label || label === '—') return '—';
  const match = label.match(/^(\d+)\s*MB\s*\/\s*(\d+)\s*MB$/);
  if (!match) return label;
  const usedMb = parseInt(match[1], 10);
  const totalMb = parseInt(match[2], 10);
  if (totalMb <= MEMORY_GB_THRESHOLD_MB) return label;
  const fmtGb = (mb: number) => {
    const gb = mb / 1024;
    return gb >= 10 ? `${Math.round(gb)} GB` : `${gb.toFixed(1)} GB`;
  };
  return `${fmtGb(usedMb)} / ${fmtGb(totalMb)}`;
}

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
