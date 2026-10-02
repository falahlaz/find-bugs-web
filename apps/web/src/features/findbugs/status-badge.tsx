import { Badge } from '@/components/ui/badge'
import { isRunning, severityTone, statusInfo, type JobStatus } from './status'

export function StatusBadge({ status, className }: { status: JobStatus; className?: string }) {
  const info = statusInfo[status]
  return (
    <Badge tone={info.tone} dot pulse={isRunning(status)} className={className}>
      {info.label}
    </Badge>
  )
}

const severityLabel: Record<string, string> = { low: 'Low', medium: 'Medium', high: 'High', critical: 'Critical' }

export function SeverityBadge({ severity }: { severity?: string }) {
  if (!severity) return null
  return (
    <Badge tone={severityTone[severity] ?? 'neutral'} dot>
      {severityLabel[severity] ?? severity}
    </Badge>
  )
}
