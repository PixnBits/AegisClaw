import { describe, it, expect } from 'vitest';
import { formatPersonaLabel } from '@/lib/display';

describe('formatPersonaLabel', () => {
  it('strips court-persona prefix and title-cases', () => {
    expect(formatPersonaLabel('court-persona-ciso')).toBe('CISO');
    expect(formatPersonaLabel('court-persona-security-architect')).toBe('Security Architect');
  });

  it('handles user roles', () => {
    expect(formatPersonaLabel('user:alice')).toBe('alice');
    expect(formatPersonaLabel('user')).toBe('You');
  });

  it('handles project roles', () => {
    expect(formatPersonaLabel('project-manager-main')).toBe('Project Manager Main');
  });
});
