import { useSystemStatus } from '@/app/use-system-status'
import { Card, CardContent } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'
import { SubmitForm } from './submit-form'

export function SubmitPage() {
  const status = useSystemStatus()
  const queue = status.data?.queue
  return (
    <div className="grid max-w-3xl gap-4">
      <PageHeader
        title="Investigasi baru"
        description={`Sistem mencari log di Splunk, lalu AI membuat diagnosis.${queue ? ` Antrean ${queue.active}/${queue.max}.` : ''}`}
      />
      <Card>
        <CardContent>
          <SubmitForm />
        </CardContent>
      </Card>
    </div>
  )
}
