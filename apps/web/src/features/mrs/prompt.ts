import type { Schemas } from '@/lib/api'
import type { MR } from './queries'

type Comment = Schemas['MRCommentView']

/**
 * What the session prompt knows besides the MR. files is undefined when the
 * local conflict check was not run or failed (conflictError says why).
 */
export type PromptContext = {
  comments: Comment[]
  files?: string[]
  conflictError?: string
}

function quote(body: string) {
  return body
    .trim()
    .split('\n')
    .map((l) => `   > ${l}`)
    .join('\n')
}

/**
 * The first message for a Remote Control session in the MR's repo: the MR,
 * how to get onto its branch, the conflicts and the unresolved comments to
 * fix, and to stop before pushing.
 */
export function sessionPrompt(mr: MR, ctx: PromptContext): string {
  const lines: string[] = [
    `Tolong bereskan MR ${mr.project}!${mr.iid} di repo ini.`,
    '',
    `MR: ${mr.title}`,
    `Link: ${mr.webUrl}`,
    `Branch: ${mr.source} -> ${mr.target}`,
    '',
    '## Persiapan',
    '1. Cek `git status`. Kalau ada perubahan yang belum di-commit, berhenti dan tanya saya dulu.',
    `2. \`git fetch origin ${mr.source} ${mr.target}\`, lalu checkout \`${mr.source}\` dan samakan dengan \`origin/${mr.source}\`.`,
  ]

  let step = 3
  if (mr.hasConflicts || (ctx.files?.length ?? 0) > 0) {
    lines.push(`${step++}. Merge \`origin/${mr.target}\` ke \`${mr.source}\` (jangan rebase, branch-nya sudah di-push).`)
    lines.push('', '## Konflik')
    if (ctx.files?.length) {
      lines.push(`File yang konflik (dicek dengan git merge-tree):`, ...ctx.files.map((f) => `- ${f}`))
    } else {
      lines.push(ctx.conflictError ? `Daftar file belum bisa dicek (${ctx.conflictError}); lihat hasil merge-nya.` : 'Lihat file yang konflik dari hasil merge.')
    }
    lines.push(
      `Selesaikan konfliknya dengan mempertahankan perubahan MR ini sekaligus perubahan terbaru di ${mr.target}. Kalau ada yang ambigu, tanya saya.`,
    )
  }

  if (ctx.comments.length > 0) {
    lines.push('', '## Komentar review yang belum resolved')
    ctx.comments.forEach((c, i) => {
      const where = c.path ? `${c.path}:${c.line ?? ''}` : 'thread umum'
      const replies = c.replies > 0 ? ` (${c.replies} balasan, cek di link MR)` : ''
      lines.push(`${i + 1}. ${where}, dari ${c.author}${replies}:`, quote(c.body))
    })
    lines.push('Kerjakan tiap komentar. Kalau ada yang menurutmu tidak perlu diubah, jelaskan alasannya, jangan diubah.')
  }

  lines.push(
    '',
    '## Selesai',
    '- Jalankan test/lint yang relevan di repo ini.',
    '- Commit dengan pesan yang jelas, tanpa atribusi AI (tanpa Co-Authored-By atau link sesi).',
    '- Jangan push. Berhenti, tunjukkan ringkasan perubahan dan hasil test, lalu tunggu konfirmasi saya.',
  )
  return lines.join('\n')
}
