import { Card, CardContent } from '@/components/ui/card'

// Placeholder stands in for routes that exist in the URL model but
// haven't been built yet. Tells the operator which Phase D sub-commit
// will bring the page online.
export default function Placeholder({ title, phase }: { title: string; phase: string }) {
  return (
    <div className="container mx-auto p-4 sm:p-6">
      <Card>
        <CardContent className="p-8 text-center text-muted-foreground">
          <h2 className="text-xl font-semibold mb-2">{title}</h2>
          <p className="text-sm">Lands in Phase {phase}.</p>
        </CardContent>
      </Card>
    </div>
  )
}
