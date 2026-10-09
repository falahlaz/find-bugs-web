import { Archive, History, Pencil } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { Segmented } from '@/components/ui/segmented'
import { useTimezone } from '@/features/findbugs/queries'
import { formatDateTime } from '@/lib/format'
import { VerdictBadge } from './engineer-note-card'
import { NoteBody, NoteEditor } from './note-editor'
import { type EngineerNote, type NoteState, useArchiveNote, useNotes, useNoteVersions } from './queries'

function Versions({ id }: { id: number }) {
  const tz = useTimezone()
  const versions = useNoteVersions(id, true)
  if (versions.error) return <Alert tone="danger">{versions.error.message}</Alert>
  if (!versions.data) return <div className="skeleton h-16 w-full" />
  return (
    <ol className="grid gap-3">
      {versions.data.map((v) => (
        <li key={v.id} className="grid gap-2 rounded-lg border p-3">
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            {formatDateTime(v.createdAt, tz)} · {v.username || 'sistem'}
            {v.aiVerdict !== 'ok' && <VerdictBadge verdict={v.aiVerdict as EngineerNote['aiVerdict']} />}
          </div>
          <NoteBody causeText={v.causeText} qaText={v.qaText} />
        </li>
      ))}
    </ol>
  )
}

function NoteRow({ note }: { note: EngineerNote }) {
  const tz = useTimezone()
  const [mode, setMode] = useState<'view' | 'edit' | 'history'>('view')
  const archive = useArchiveNote()
  const active = note.state === 'active'
  return (
    <Card>
      <CardContent className="grid gap-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="grid min-w-0 gap-1.5">
            <code className="text-[13px] [overflow-wrap:anywhere]">{note.component}</code>
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <Badge tone="outline">{note.errorTypeKey === '*' ? 'Semua error type' : note.errorType}</Badge>
              <VerdictBadge verdict={note.aiVerdict} />
              <span>
                {note.updatedBy || note.createdBy} · {formatDateTime(note.updatedAt, tz)}
              </span>
              {note.sourceJobId && (
                <Link to={`/jobs/${note.sourceJobId}`} className="text-primary hover:underline">
                  job #{note.sourceJobId}
                </Link>
              )}
            </div>
          </div>
          <div className="flex gap-1">
            {active && (
              <Button size="sm" variant="ghost" onClick={() => setMode(mode === 'edit' ? 'view' : 'edit')}>
                <Pencil />
                Edit
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => setMode(mode === 'history' ? 'view' : 'history')}>
              <History />
              Riwayat
            </Button>
            {active && (
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
            )}
          </div>
        </div>
        {archive.error && <Alert tone="danger">{archive.error.message}</Alert>}
        {mode === 'edit' ? (
          <NoteEditor
            noteId={note.id}
            initial={{ cause: note.cause, qaRecommendation: note.qaRecommendation }}
            onDone={() => setMode('view')}
            onCancel={() => setMode('view')}
          />
        ) : mode === 'history' ? (
          <Versions id={note.id} />
        ) : (
          <NoteBody causeText={note.causeText} qaText={note.qaText} />
        )}
      </CardContent>
    </Card>
  )
}

export function NotesPage() {
  const [state, setState] = useState<NoteState>('active')
  const notes = useNotes(state)
  return (
    <div className="grid gap-4">
      <PageHeader
        title="Rekomendasi engineer"
        description="Penyebab dan saran untuk QA yang ditulis engineer. Muncul otomatis di setiap job dengan komponen dan error type yang sama."
        actions={
          <Segmented<NoteState>
            label="Status"
            value={state}
            onChange={setState}
            options={[
              { value: 'active', label: 'Aktif' },
              { value: 'archived', label: 'Arsip' },
            ]}
          />
        }
      />
      {notes.error && <Alert tone="danger">{notes.error.message}</Alert>}
      {notes.isPending && <div className="skeleton h-28 w-full" />}
      {notes.data?.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {state === 'active' ? 'Belum ada rekomendasi. Tambahkan dari halaman detail job yang sudah selesai dianalisis.' : 'Belum ada rekomendasi yang diarsipkan.'}
        </p>
      )}
      {notes.data?.map((n) => <NoteRow key={n.id} note={n} />)}
    </div>
  )
}
