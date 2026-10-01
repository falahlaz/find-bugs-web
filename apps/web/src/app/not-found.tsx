import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { LinkButton } from '@/components/ui/link-button'

export function NotFound() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Halaman tidak ditemukan</CardTitle>
        <CardDescription>Link ini tidak ada.</CardDescription>
        <div>
          <LinkButton to="/" size="sm" variant="outline">
            Ke halaman Submit
          </LinkButton>
        </div>
      </CardHeader>
    </Card>
  )
}
