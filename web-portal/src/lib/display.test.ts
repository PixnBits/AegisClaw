import { describe, it, expect } from 'vitest';
import { formatPersonaLabel, formatMemoryUsage } from '@/lib/display';

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

describe('formatMemoryUsage', () => {
  it('keeps MB for small totals', () => {
    expect(formatMemoryUsage('512 MB / 1024 MB')).toBe('512 MB / 1024 MB');
  });

  it('formats as GB above 1.25 GB total', () => {
    expect(formatMemoryUsage('20338 MB / 128084 MB')).toBe('20 GB / 125 GB');
  });
});
