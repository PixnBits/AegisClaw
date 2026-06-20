import { describe, it, expect } from 'vitest';
import { dedupeMembers, groupMembers } from '@/lib/members';

describe('dedupeMembers', () => {
  it('collapses project-manager variants', () => {
    const members = [
      { role: 'project-manager' },
      { role: 'project-manager-main' },
      { role: 'coder' },
    ];
    expect(dedupeMembers(members).map((m) => m.role)).toEqual(['project-manager', 'coder']);
  });
});

describe('groupMembers', () => {
  it('dedupes before grouping', () => {
    const groups = groupMembers([
      { role: 'project-manager' },
      { role: 'project-manager-main' },
    ]);
    expect(groups['Project / SDLC']).toHaveLength(1);
  });
});
