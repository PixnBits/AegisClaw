// Same rule as channelid.ValidateChannelID (MaxChannelIDLen = 45).
const channelIDRe = /^[a-z][a-z0-9-]*$/;
const maxChannelIDLen = 45;
const channelIDErrorText =
  'invalid channel id: must match ^[a-z][a-z0-9-]*$ and be <= 45 chars';

/** Returns the shared invalid-id message, or null when id may be created. */
export function validateChannelId(id: string): string | null {
  if (!channelIDRe.test(id) || id.includes('--') || id.endsWith('-') || id.length > maxChannelIDLen) {
    return channelIDErrorText;
  }
  return null;
}
