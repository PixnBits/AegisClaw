import { describe, expect, it } from 'vitest';
import {
  PROPOSAL_NOTE_MAX_LEN,
  sanitizeLinkHref,
  sanitizeProposalNote,
  sanitizeText,
} from './sanitize';

describe('sanitizeProposalNote', () => {
  it('returns undefined for empty input', () => {
    expect(sanitizeProposalNote('')).toBeUndefined();
    expect(sanitizeProposalNote('   ')).toBeUndefined();
    expect(sanitizeProposalNote(undefined)).toBeUndefined();
  });

  it('strips HTML tags', () => {
    expect(sanitizeProposalNote('<script>x</script>ok')).toBe('ok');
    expect(sanitizeProposalNote('hello <b>world</b>')).toBe('hello world');
  });

  it('strips control characters', () => {
    expect(sanitizeProposalNote('a\u0000b')).toBe('ab');
  });

  it('caps length', () => {
    const long = 'x'.repeat(PROPOSAL_NOTE_MAX_LEN + 50);
    expect(sanitizeProposalNote(long)?.length).toBe(PROPOSAL_NOTE_MAX_LEN);
  });
});

describe('sanitizeText', () => {
  it('redacts secrets and internal paths in trace context', () => {
    const out = sanitizeText('trace', 'api_key: sk-live-abc read /etc/passwd host 10.0.0.5');
    expect(out).toContain('[REDACTED]');
    expect(out).not.toContain('/etc/passwd');
    expect(out).not.toContain('10.0.0.5');
  });

  it('escapes script tags in chat context', () => {
    const out = sanitizeText('chat', '<script>alert(1)</script> hi');
    expect(out).not.toContain('<script');
    expect(out).toContain('&lt;script');
  });

  it('truncates very long trace content', () => {
    const out = sanitizeText('trace', 'x'.repeat(9000));
    expect(out.length).toBeLessThan(8100);
    expect(out.endsWith('…')).toBe(true);
  });
});

describe('sanitizeLinkHref', () => {
  it('allows safe protocols', () => {
    expect(sanitizeLinkHref('https://example.com')).toBe('https://example.com');
    expect(sanitizeLinkHref('#section')).toBe('#section');
  });

  it('blocks javascript URLs', () => {
    expect(sanitizeLinkHref('javascript:alert(1)')).toBeNull();
    expect(sanitizeLinkHref('data:text/html,evil')).toBeNull();
  });
});