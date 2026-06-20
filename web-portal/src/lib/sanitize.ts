/** Max length for optional proposal action notes sent to the bridge. */
export const PROPOSAL_NOTE_MAX_LEN = 2000;

const HTML_TAG = /<[^>]*>/g;
const SCRIPT_BLOCK = /<script\b[^>]*>[\s\S]*?<\/script>/gi;
const CONTROL_CHARS = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g;

/**
 * Sanitize user-supplied proposal action notes before JSON POST.
 * Strips HTML/control characters and caps length (mirrors server-side redaction intent).
 */
export function sanitizeProposalNote(raw: string | undefined | null): string | undefined {
  if (raw == null) return undefined;
  let s = raw.replace(CONTROL_CHARS, '').replace(SCRIPT_BLOCK, '').replace(HTML_TAG, '').trim();
  if (!s) return undefined;
  if (s.length > PROPOSAL_NOTE_MAX_LEN) {
    s = s.slice(0, PROPOSAL_NOTE_MAX_LEN);
  }
  return s;
}
