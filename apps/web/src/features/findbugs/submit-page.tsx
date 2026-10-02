import { useSystemStatus } from '@/app/use-system-status'
import { Card, CardContent } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { SubmitForm } from './submit-form'

const steps = [
  ['Antre', 'Maks 3 job berjalan per user.'],
  ['Cek VPN', 'Job menunggu kalau VPN putus.'],
  ['Cari di Splunk', 'Termasuk _id backend terkait.'],
  ['Analisis AI', 'Diagnosis dalam Bahasa Indonesia.'],
]

export function SubmitPage() {
  const status = useSystemStatus()
  const queue = status.data?.queue
  return (
    <div className="grid max-w-3xl gap-4">
      <PageHeader
        title="Investigasi baru"
        description={`Paste transaction ID atau curl lengkap. Sistem mencari log di Splunk, lalu AI membuat diagnosis.${queue ? ` Antrean ${queue.active}/${queue.max}.` : ''}`}
      />
      <Card>
        <CardContent>
          <SubmitForm />
        </CardContent>
      </Card>
      <ol className="grid grid-cols-2 gap-2 sm:grid-cols-4">
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
