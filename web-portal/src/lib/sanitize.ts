/** Max length for optional proposal action notes sent to the bridge. */
export const PROPOSAL_NOTE_MAX_LEN = 2000;

/** Context-aware sanitization per security-boundaries.md */
export type SanitizeContext = 'chat' | 'trace' | 'proposal';

const HTML_TAG = /<[^>]*>/g;
const SCRIPT_BLOCK = /<script\b[^>]*>[\s\S]*?<\/script>/gi;
const CONTROL_CHARS = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g;
const API_KEY = /(api[_-]?key|secret|password|token|bearer)\s*[:=]\s*\S+/gi;
const CREDENTIAL = /(AKIA[0-9A-Z]{16}|sk-[a-zA-Z0-9]{20,})/gi;
const INTERNAL_PATH = /\/(etc|var|opt|proc|sys|home|root)\/[^\s]*/g;
const PRIVATE_IP =
  /\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b/g;
const INTERNAL_HOST = /\b[a-zA-Z0-9-]+\.(internal|local|svc|cluster)\b/g;

const REDACTED = '[REDACTED]';
const TRACE_MAX_LEN = 8000;

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

/** Context-aware plain-text redaction before Markdown rendering. */
export function sanitizeText(ctx: SanitizeContext, raw: string): string {
  if (!raw) return '';
  let s = raw;
  s = s.replace(API_KEY, (match, label: string) => `${label || match.split(/[:=]/)[0].trim()}: ${REDACTED}`);
  s = s.replace(CREDENTIAL, REDACTED);
  s = s.replace(INTERNAL_PATH, REDACTED);
  s = s.replace(PRIVATE_IP, REDACTED);
  s = s.replace(INTERNAL_HOST, REDACTED);

  switch (ctx) {
    case 'chat':
      s = s.replace(/<script/gi, '&lt;script').replace(/<\/script/gi, '&lt;/script');
      s = s.replace(/<iframe/gi, '&lt;iframe');
      break;
    case 'trace':
      if (s.length > TRACE_MAX_LEN) {
        s = s.slice(0, TRACE_MAX_LEN) + '…';
      }
      break;
    case 'proposal':
      break;
  }
  return s;
}

/** Allowed link protocols for rendered Markdown. */
export function sanitizeLinkHref(href: string): string | null {
  const trimmed = (href || '').trim();
  if (!trimmed) return null;
  const lower = trimmed.toLowerCase();
  if (lower.startsWith('javascript:') || lower.startsWith('data:') || lower.startsWith('vbscript:')) {
    return null;
  }
  if (lower.startsWith('http://') || lower.startsWith('https://') || lower.startsWith('mailto:')) {
    return trimmed;
  }
  if (trimmed.startsWith('#') || trimmed.startsWith('/')) {
    return trimmed;
  }
  return null;
}