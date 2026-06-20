import { useMemo } from 'react';
import { marked } from 'marked';
import DOMPurify from 'dompurify';
import './MarkdownContent.css';

marked.setOptions({ breaks: true, gfm: true });

type Props = {
  content: string;
  className?: string;
};

export function MarkdownContent({ content, className }: Props) {
  const html = useMemo(() => {
    const raw = marked.parse(content || '', { async: false }) as string;
    return DOMPurify.sanitize(raw, {
      USE_PROFILES: { html: true },
    });
  }, [content]);

  return (
    <div
      className={`markdown-content${className ? ` ${className}` : ''}`}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
