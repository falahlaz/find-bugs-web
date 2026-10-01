import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

/** Stand-in for pages built in phase 2 and 3. */
export function Placeholder({ title, phase }: { title: string; phase: number }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>Halaman ini dibangun di Fase {phase}. API-nya sudah tersedia di backend.</CardDescription>
      </CardHeader>
    </Card>
  )
}
