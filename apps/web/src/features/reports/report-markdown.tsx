import ReactMarkdown, { type Components } from 'react-markdown'
import { Link } from 'react-router'
import remarkGfm from 'remark-gfm'

const frontmatter = /^---\r?\n[\s\S]*?\r?\n---\r?\n?/

/**
 * Maps a link inside report.md to a route: another report.md in the
 * knowledge base opens on this page, everything else stays a plain link.
 */
function reportRoute(href: string, repo: string, slug: string): string | null {
  if (/^[a-z]+:/i.test(href) || href.startsWith('#') || href.startsWith('/')) return null
  const url = new URL(href, `https://kb/${repo}/${slug}/report.md`)
  const m = /^\/([^/]+)\/([^/]+)\/report\.(md|html)$/.exec(url.pathname)
  return m ? `/laporan/${m[1]}/${m[2]}` : null
}

/**
 * Renders report.md with GitHub-flavoured Markdown (tables, task lists).
 * Raw HTML in the file is not rendered.
 */
export function ReportMarkdown({ text, repo, slug }: { text: string; repo: string; slug: string }) {
  const components: Components = {
    a: ({ href = '', children }) => {
      const to = reportRoute(href, repo, slug)
      if (to) return <Link to={to}>{children}</Link>
      if (href.startsWith('#')) return <span className="text-foreground">{children}</span>
      return (
        <a href={href} target="_blank" rel="noreferrer">
          {children}
        </a>
      )
    },
    table: ({ children }) => (
      <div className="overflow-x-auto">
        <table>{children}</table>
      </div>
    ),
  }
  return (
    <div className="report-md">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {text.replace(frontmatter, '')}
      </ReactMarkdown>
    </div>
  )
}
