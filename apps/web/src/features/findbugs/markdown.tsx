import { Fragment, type ReactNode } from 'react'

/**
 * A small Markdown subset (code fences, headings, bullet and numbered
 * lists, inline code, bold) rendered as React elements, never as HTML:
 * AI answers quote untrusted logs and code.
 */
export function Markdown({ text }: { text: string }) {
  const parts = text.split(/^```[^\n]*\n?/m)
  return (
    <div className="grid grid-cols-1 gap-2">
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <pre key={i} className="overflow-auto rounded-md bg-muted px-3 py-2 font-mono text-[12.5px] leading-relaxed">
            {part.replace(/\n$/, '')}
          </pre>
        ) : (
          blocks(part).map((b, j) => <Block key={`${i}-${j}`} block={b} />)
        ),
      )}
    </div>
  )
}

type MdBlock = { kind: 'p'; text: string } | { kind: 'h'; text: string } | { kind: 'ul' | 'ol'; items: string[] }

const bullet = /^\s*[-*•]\s+/
const numbered = /^\s*\d+[.)]\s+/

/** Splits prose into paragraphs, headings and lists, line by line. */
function blocks(s: string): MdBlock[] {
  const out: MdBlock[] = []
  let para: string[] = []
  const flush = () => {
    if (para.length) out.push({ kind: 'p', text: para.join('\n') })
    para = []
  }
  for (const line of s.split('\n')) {
    const list = bullet.test(line) ? 'ul' : numbered.test(line) ? 'ol' : null
    if (line.trim() === '') {
      flush()
    } else if (/^#{1,6}\s+/.test(line)) {
      flush()
      out.push({ kind: 'h', text: line.replace(/^#{1,6}\s+/, '') })
    } else if (list) {
      // A paragraph in between (e.g. a bold heading line) starts a new list.
      const continues = para.length === 0 && out[out.length - 1]?.kind === list
      flush()
      const item = line.replace(list === 'ul' ? bullet : numbered, '')
      const last = out[out.length - 1]
      if (continues && last && last.kind === list) last.items.push(item)
      else out.push({ kind: list, items: [item] })
    } else if (para.length === 0 && /^\s+/.test(line) && isList(out[out.length - 1])) {
      // An indented line continues the previous list item.
      const last = out[out.length - 1] as { items: string[] }
      last.items[last.items.length - 1] += ' ' + line.trim()
    } else {
      para.push(line)
    }
  }
  flush()
  return out
}

function isList(b: MdBlock | undefined): b is { kind: 'ul' | 'ol'; items: string[] } {
  return b?.kind === 'ul' || b?.kind === 'ol'
}

const wrap = 'whitespace-pre-wrap [overflow-wrap:anywhere]'

function Block({ block }: { block: MdBlock }) {
  switch (block.kind) {
    case 'h':
      return <p className="mt-1 font-semibold">{inline(block.text.trim())}</p>
    case 'ul':
    case 'ol': {
      const List = block.kind
      return (
        <List className={`grid gap-1 pl-5 ${block.kind === 'ul' ? 'list-disc' : 'list-decimal'} marker:text-muted-foreground`}>
          {block.items.map((item, k) => (
            <li key={k} className={wrap}>
              {inline(item.trim())}
            </li>
          ))}
        </List>
      )
    }
    default:
      return <p className={wrap}>{inline(block.text.trim())}</p>
  }
}

function inline(s: string): ReactNode[] {
  return s.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*)/).map((t, i) => {
    if (t.startsWith('`') && t.endsWith('`') && t.length > 2) {
      return (
        <code key={i} className="rounded bg-muted px-1 py-0.5 font-mono text-[12.5px]">
          {t.slice(1, -1)}
        </code>
      )
    }
    if (t.startsWith('**') && t.endsWith('**') && t.length > 4) return <strong key={i}>{t.slice(2, -2)}</strong>
    return <Fragment key={i}>{t}</Fragment>
  })
}
