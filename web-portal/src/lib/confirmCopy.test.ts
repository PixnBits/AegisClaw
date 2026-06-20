import { describe, expect, it } from 'vitest';
import { isProposalAction, proposalActionConfirmCopy } from './confirmCopy';

describe('isProposalAction', () => {
  it('accepts bridge allow-list actions', () => {
    expect(isProposalAction('approve')).toBe(true);
    expect(isProposalAction('reject')).toBe(true);
    expect(isProposalAction('defer')).toBe(true);
  });

  it('rejects unknown actions', () => {
    expect(isProposalAction('delete')).toBe(false);
    expect(isProposalAction('')).toBe(false);
  });
});

describe('proposalActionConfirmCopy', () => {
  it('includes proposal title in message', () => {
    const copy = proposalActionConfirmCopy('approve', 'Skill X');
    expect(copy.message).toContain('Skill X');
  });
});
