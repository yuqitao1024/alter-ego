'use client'

import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

export function MarkdownContent({
  content,
  compact = false,
  clampLines
}: {
  content: string
  compact?: boolean
  clampLines?: number
}) {
  const text = content.trim()
  if (!text) {
    return null
  }

  return (
    <div
      className={`markdown-content ${compact ? 'markdown-content--compact' : ''}`}
      style={
        clampLines
          ? {
              display: '-webkit-box',
              WebkitBoxOrient: 'vertical',
              WebkitLineClamp: clampLines,
              overflow: 'hidden'
            }
          : undefined
      }
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ ...props }) => <a {...props} target="_blank" rel="noreferrer" />,
          code: ({ className, children, ...props }) => {
            const isBlock = Boolean(className)
            if (!isBlock) {
              return (
                <code className="rounded-md bg-white/[0.08] px-1.5 py-0.5 text-[0.92em] text-[rgba(220,236,248,0.95)]" {...props}>
                  {children}
                </code>
              )
            }
            return (
              <code className={className} {...props}>
                {children}
              </code>
            )
          },
          pre: ({ children }) => (
            <pre className="overflow-x-auto rounded-2xl border border-white/10 bg-[rgba(3,8,14,0.82)] px-4 py-3 text-[13px] leading-6 text-[rgba(214,230,242,0.94)]">
              {children}
            </pre>
          )
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
