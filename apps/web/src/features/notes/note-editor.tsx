import { CircleCheck, LoaderCircle, MessageCircleQuestion, Sparkles } from 'lucide-react'
import { useState } from 'react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { FieldLabel } from '@/features/findbugs/result-view'
import { Markdown } from '@/features/findbugs/markdown'
import { ApiError } from '@/lib/api'
import { type NoteReview, useCheckNote, useSaveNote } from './queries'

/** What QA sees: the cause and the recommendation rewritten for them. */
export function NoteBody({ causeText, qaText }: { causeText: string; qaText: string }) {
  return (
    <div className="grid gap-3">
      <div className="grid gap-1">
        <FieldLabel>Penyebab</FieldLabel>
        <p className="max-w-[68ch] text-sm leading-relaxed whitespace-pre-wrap">{causeText}</p>
      </div>
      <div className="grid gap-1">
        <FieldLabel>Yang bisa dilakukan QA</FieldLabel>
        <div className="max-w-[68ch] text-sm leading-relaxed">
          <Markdown text={qaText} />
        </div>
      </div>
    </div>
  )
}

function List({ icon, title, items }: { icon: React.ReactNode; title: string; items: string[] }) {
  if (items.length === 0) return null
  return (
    <div className="grid gap-1.5">
      <div className="flex items-center gap-1.5 text-sm font-medium">
        {icon}
        {title}
      </div>
      <ul className="grid list-disc gap-1 pl-5 text-sm">
        {items.map((q) => (
          <li key={q}>{q}</li>
        ))}
      </ul>
    </div>
  )
}

/**
 * Writes or edits an engineer note: the AI reviews the answers first, then
 * the engineer saves its rewrite, revises, or saves anyway.
 */
export function NoteEditor({
  jobId,
  noteId,
  component,
  initial,
  onDone,
  onCancel,
}: {
  /** The job the note is written on; optional when editing a note. */
  jobId?: number
  noteId?: number
  /** The failed component, shown when a new note may cover all its errors. */
  component?: string
  initial?: { cause: string; qaRecommendation: string }
  onDone: () => void
  onCancel: () => void
}) {
  const [cause, setCause] = useState(initial?.cause ?? '')
  const [qa, setQa] = useState(initial?.qaRecommendation ?? '')
  const [allTypes, setAllTypes] = useState(false)
  const [review, setReview] = useState<NoteReview | null>(null)
  const check = useCheckNote()
  const save = useSaveNote()

  const reviewDown = check.error instanceof ApiError && (check.error.code === 'review_unavailable' || check.error.code === 'review_failed')
  const busy = check.isPending || save.isPending

  function submit(e: React.FormEvent) {
    e.preventDefault()
    save.reset()
    check.mutate({ jobId, noteId, cause, qaRecommendation: qa }, { onSuccess: setReview })
  }

  function store(aiVerdict: 'ok' | 'overridden' | 'unchecked') {
    save.mutate(
      {
        jobId,
        noteId,
        cause,
        qaRecommendation: qa,
        causeText: review?.causeText,
        qaText: review?.qaText,
        aiVerdict,
        allErrorTypes: noteId ? undefined : allTypes,
      },
      { onSuccess: onDone },
    )
  }

  if (review) {
    const ok = review.verdict === 'ok'
    return (
      <div className="grid gap-4">
        {ok ? (
          <Alert tone="info">AI menilai jawabanmu sudah jelas. Begini nanti yang dilihat QA:</Alert>
        ) : (
          <Alert tone="warning">AI menemukan bagian yang masih ambigu. Sebaiknya revisi dulu, atau tetap simpan kalau kamu yakin.</Alert>
        )}
        <List icon={<MessageCircleQuestion className="size-4 text-warn" />} title="Pertanyaan dari AI" items={review.questions} />
        <List icon={<Sparkles className="size-4 text-primary" />} title="Saran perbaikan" items={review.suggestions} />
        <div className="grid gap-2 rounded-lg border bg-secondary/40 p-3.5">
          <div className="text-xs font-medium text-muted-foreground">Pratinjau untuk QA</div>
          <NoteBody causeText={review.causeText} qaText={review.qaText} />
        </div>
        {save.error && <Alert tone="danger">{save.error.message}</Alert>}
        <div className="flex flex-wrap gap-2">
          {ok ? (
            <>
              <Button size="sm" disabled={busy} onClick={() => store('ok')}>
                {save.isPending ? <LoaderCircle className="animate-spin" /> : <CircleCheck />}
                Simpan
              </Button>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => setReview(null)}>
                Ubah lagi
              </Button>
            </>
          ) : (
            <>
              <Button size="sm" disabled={busy} onClick={() => setReview(null)}>
                Revisi jawaban
              </Button>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => store('overridden')}>
                {save.isPending && <LoaderCircle className="animate-spin" />}
                Tetap simpan
              </Button>
            </>
          )}
        </div>
      </div>
    )
  }

  return (
    <form className="grid gap-4" onSubmit={submit}>
      <div className="grid gap-1.5">
        <Label htmlFor="note-cause">Penyebab error</Label>
        <Textarea
          id="note-cause"
          rows={4}
          maxLength={4000}
          required
          value={cause}
          onChange={(e) => setCause(e.target.value)}
          placeholder="Kenapa error ini terjadi? Sebutkan komponen, kondisi, atau data yang memicunya."
        />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="note-qa">Rekomendasi untuk QA</Label>
        <Textarea
          id="note-qa"
          rows={4}
          maxLength={4000}
          required
          value={qa}
          onChange={(e) => setQa(e.target.value)}
          placeholder="Apa yang sebaiknya QA lakukan saat error ini muncul, dan kapan harus eskalasi?"
        />
      </div>
      {!noteId && component && (
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" checked={allTypes} onChange={(e) => setAllTypes(e.target.checked)} className="mt-0.5 size-4 accent-primary" />
          <span>
            Berlaku untuk semua error type di komponen ini
            <code className="mt-1 block text-xs text-muted-foreground [overflow-wrap:anywhere]">{component}</code>
          </span>
        </label>
      )}
      {check.error && (
        <Alert tone={reviewDown ? 'warning' : 'danger'}>
          {check.error.message}
          {reviewDown && (
            <Button type="button" size="sm" variant="outline" className="mt-2 flex" disabled={busy} onClick={() => store('unchecked')}>
              Simpan tanpa cek AI
            </Button>
          )}
        </Alert>
      )}
      {save.error && <Alert tone="danger">{save.error.message}</Alert>}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" size="sm" disabled={busy || !cause.trim() || !qa.trim()}>
          {check.isPending ? <LoaderCircle className="animate-spin" /> : <Sparkles />}
          {check.isPending ? 'AI sedang mengecek…' : 'Cek dengan AI'}
        </Button>
        <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={onCancel}>
          Batal
        </Button>
      </div>
    </form>
  )
}
