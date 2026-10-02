import { useSystemStatus } from '@/app/use-system-status'
import { useDesign } from '@/app/design'
import { Card, CardContent } from '@/components/ui/card'
import { LinkButton } from '@/components/ui/link-button'
import { BentoHome } from './bento-home'
import { JobRows } from './job-list-views'
import { useJobs } from './queries'
import { SubmitForm } from './submit-form'

const steps = [
  ['Antre', 'Maks 3 job berjalan per user.'],
  ['Cek VPN', 'Job menunggu kalau VPN putus.'],
  ['Cari di Splunk', 'Termasuk _id backend terkait.'],
  ['Analisis AI', 'Diagnosis dalam Bahasa Indonesia.'],
]

export function SubmitPage() {
  const { design } = useDesign()
  const status = useSystemStatus()
  const queue = status.data?.queue
  if (design === 'bento') return <BentoHome />

  const intro = (
    <div>
      <h1 className="font-display text-xl font-semibold tracking-tight">Investigasi baru</h1>
      <p className="mt-1 text-sm text-muted-foreground">
        Paste transaction ID atau curl lengkap. Sistem mencari log di Splunk, lalu AI membuat diagnosis.
        {queue && ` Antrean ${queue.active}/${queue.max}.`}
      </p>
    </div>
  )

  if (design === 'triage') {
    return (
      <div className="grid max-w-3xl gap-4">
        {intro}
        <Card>
          <CardContent>
            <SubmitForm variant="plain" />
          </CardContent>
        </Card>
        <ol className="grid gap-2 sm:grid-cols-4">
          {steps.map(([title, hint], i) => (
            <li key={title} className="grid gap-0.5 rounded-lg border border-dashed px-3 py-2.5">
              <span className="font-mono text-[11px] text-muted-foreground">{i + 1}</span>
              <span className="text-[13px] font-medium">{title}</span>
              <span className="text-xs text-muted-foreground">{hint}</span>
            </li>
          ))}
        </ol>
      </div>
    )
  }

  return (
    <div className="grid gap-7">
      {intro}
      <SubmitForm />
      <RecentMine />
    </div>
  )
}

function RecentMine() {
  const jobs = useJobs({ mine: '1' })
  const list = (jobs.data ?? []).slice(0, 5)
  if (!list.length) return null
  return (
    <section className="grid gap-2.5">
      <div className="flex items-center justify-between text-xs font-medium text-muted-foreground">
        <span>Job terakhir saya</span>
        <LinkButton to="/jobs" variant="ghost" size="sm" className="h-7 text-xs">
          Lihat semua →
        </LinkButton>
      </div>
      <JobRows jobs={list} showUser={false} />
    </section>
  )
}
