import { describe, expect, it } from 'vitest';
import { validateChannelId } from './channelId';

// Same rule as internal/channelid.ValidateChannelID.
describe('validateChannelId', () => {
  it.each([
    ['main'],
    ['plan-demo'],
    ['a'],
    ['task-refactorauthenticationmodule'],
    ['q4-plan-2'],
    ['a'.repeat(45)],
  ])('accepts %s', (id) => {
    expect(validateChannelId(id)).toBeNull();
  });

  it.each([
    ['double dash', 'has--dash'],
    ['trailing dash', 'trailing-'],
    ['too long (46)', 'a'.repeat(46)],
    ['underscore', 'q4_plan'],
    ['dot', 'v1.2'],
    ['leading digit', '4plan'],
    ['uppercase', 'MyProj'],
    ['leading dash', '-plan'],
    ['empty', ''],
    ['space', 'my plan'],
  ])('rejects %s', (_label, id) => {
    expect(validateChannelId(id)).toMatch(/^invalid channel id/);
  });
});
