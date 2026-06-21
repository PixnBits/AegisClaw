import { useMemo } from 'react';
import { marked } from 'marked';
import DOMPurify from 'dompurify';
import { sanitizeText, type SanitizeContext } from '@/lib/sanitize';
import './MarkdownContent.css';

marked.setOptions({ breaks: true, gfm: true });

type Props = {
  content: string;
  className?: string;
  /** Context-aware redaction before Markdown parse (default: chat). */
  context?: SanitizeContext;
};

export function MarkdownContent({ content, className, context = 'chat' }: Props) {
  const html = useMemo(() => {
    const safe = sanitizeText(context, content || '');
    const raw = marked.parse(safe, { async: false }) as string;
    return DOMPurify.sanitize(raw, {
      USE_PROFILES: { html: true },
      ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto):|#|\/)/i,
    });
  }, [content, context]);

  return (
    <div
      className={`markdown-content${className ? ` ${className}` : ''}`}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
