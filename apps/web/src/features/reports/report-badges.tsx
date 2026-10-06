import { Badge, type BadgeTone } from '@/components/ui/badge'
import { SeverityBadge } from '@/features/findbugs/status-badge'

const statusTone: Record<string, BadgeTone> = {
  investigating: 'info',
  hypothesis: 'warning',
  'root-cause-confirmed': 'info',
  fixed: 'success',
  wontfix: 'neutral',
}

const statusLabel: Record<string, string> = {
  investigating: 'Investigasi',
  hypothesis: 'Hipotesis',
  'root-cause-confirmed': 'Root cause terkonfirmasi',
  fixed: 'Sudah fix',
  wontfix: "Won't fix",
}

export function ReportBadges({ status, severity }: { status?: string; severity?: string }) {
  return (
    <>
      {status && (
        <Badge tone={statusTone[status] ?? 'neutral'} dot>
          {statusLabel[status] ?? status}
        </Badge>
      )}
      <SeverityBadge severity={severity} />
    </>
  )
}
