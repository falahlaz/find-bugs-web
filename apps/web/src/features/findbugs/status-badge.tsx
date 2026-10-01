import { Badge } from '@/components/ui/badge'
import { severityTone, statusInfo, type JobStatus } from './status'

export function StatusBadge({ status }: { status: JobStatus }) {
  const info = statusInfo[status]
  return <Badge tone={info.tone}>{info.label}</Badge>
}

export function SeverityBadge({ severity }: { severity?: string }) {
  if (!severity) return null
  return (
    <Badge tone={severityTone[severity] ?? 'neutral'} className="uppercase">
      {severity}
    </Badge>
  )
}
