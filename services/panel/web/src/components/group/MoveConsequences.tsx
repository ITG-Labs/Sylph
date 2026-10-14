import type { ImpactLine } from "@/lib/groups"

export function MoveConsequences({ lines }: { lines: ImpactLine[] }) {
  return (
    <>
      {lines.map((l) => (
        <p key={l.text} className={l.tone === "danger" ? "font-medium" : undefined}>
          {l.text}
        </p>
      ))}
    </>
  )
}
