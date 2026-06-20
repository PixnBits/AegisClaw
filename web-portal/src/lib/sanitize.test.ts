import { describe, expect, it } from 'vitest';
import { PROPOSAL_NOTE_MAX_LEN, sanitizeProposalNote } from './sanitize';

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
