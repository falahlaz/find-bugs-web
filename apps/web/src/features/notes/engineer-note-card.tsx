import { Archive, Lightbulb, Pencil, Plus } from 'lucide-react'
import { useState } from 'react'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useTimezone } from '@/features/findbugs/queries'
import { FieldLabel, type Result } from '@/features/findbugs/result-view'
import { formatDateTime } from '@/lib/format'
import { NoteBody, NoteEditor } from './note-editor'
import { type NoteView, useArchiveNote } from './queries'

export function VerdictBadge({ verdict }: { verdict: NoteView['aiVerdict'] }) {
  if (verdict === 'ok') return null
  return <Badge tone="warning">{verdict === 'overridden' ? 'Disimpan meski AI ragu' : 'Belum dicek AI'}</Badge>
}

/**
 * The engineers' recommendation for the job's error. Everyone sees it once
 * it exists; engineers add, edit and archive it.
 */
export function EngineerNoteCard({ jobId, result, engineer }: { jobId: number; result: Result; engineer: boolean }) {
  const tz = useTimezone()
  const note = result.engineerNote ?? null
  const [editing, setEditing] = useState(false)
  const archive = useArchiveNote()

  // Without a component and error type a note cannot be matched.
  const matchable = !result.llmFailed && Boolean(result.failedComponent) && Boolean(result.errorType)
  if (!note && !(engineer && matchable)) return null

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          <Lightbulb className="size-4 text-primary" aria-hidden />
          Rekomendasi engineer
          {engineer && note && <VerdictBadge verdict={note.aiVerdict} />}
        </CardTitle>
        {note && !editing && (
          <p className="text-xs text-muted-foreground">
            {[note.updatedBy && `oleh ${note.updatedBy}`, formatDateTime(note.updatedAt, tz), note.allErrorTypes && 'berlaku untuk semua error di komponen ini']
              .filter(Boolean)
              .join(' · ')}
          </p>
        )}
      </CardHeader>
      <CardContent className="grid gap-4">
        {editing ? (
          <NoteEditor
            jobId={jobId}
            noteId={note?.id}
            component={result.failedComponent}
            initial={note ? { cause: note.cause ?? '', qaRecommendation: note.qaRecommendation ?? '' } : undefined}
            onDone={() => setEditing(false)}
            onCancel={() => setEditing(false)}
          />
        ) : note ? (
          <>
            <NoteBody causeText={note.causeText} qaText={note.qaText} />
            {engineer && (
              <>
                <details className="group">
                  <summary className="cursor-pointer text-sm text-muted-foreground select-none">Jawaban asli engineer</summary>
                  <div className="mt-2 grid gap-3 rounded-lg border p-3">
                    <div className="grid gap-1">
                      <FieldLabel>Penyebab error</FieldLabel>
                      <p className="text-sm whitespace-pre-wrap">{note.cause}</p>
                    </div>
                    <div className="grid gap-1">
                      <FieldLabel>Rekomendasi untuk QA</FieldLabel>
                      <p className="text-sm whitespace-pre-wrap">{note.qaRecommendation}</p>
                    </div>
                  </div>
                </details>
                {archive.error && <Alert tone="danger">{archive.error.message}</Alert>}
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
                    <Pencil />
                    Edit
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={archive.isPending}
                    onClick={() => {
                      if (confirm('Arsipkan rekomendasi ini? Rekomendasi tidak akan muncul lagi di job dengan error yang sama.')) archive.mutate(note.id)
                    }}
                  >
                    <Archive />
                    Arsipkan
                  </Button>
                </div>
              </>
            )}
          </>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-muted-foreground">Belum ada rekomendasi engineer untuk error ini. QA akan melihatnya di setiap job dengan error yang sama.</p>
            <Button size="sm" onClick={() => setEditing(true)}>
              <Plus />
              Tambah rekomendasi
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
