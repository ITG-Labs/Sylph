import { Construction } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"

export function PlaceholderPage({ title }: { title: string }) {
  return (
    <>
      <PageHeader title={title} />
      <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed py-16 text-muted-foreground">
        <Construction className="size-8" />
        <p>Этот экран ещё в работе</p>
      </div>
    </>
  )
}
