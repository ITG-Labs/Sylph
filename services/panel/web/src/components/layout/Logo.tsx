import { useId } from "react"

export function Logo() {
  const id = useId()
  return (
    <div className="flex items-center gap-2.5">
      <svg viewBox="0 0 32 32" className="size-8 shrink-0" aria-hidden>
        <defs>
          <linearGradient id={id} x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stopColor="#a78bfa" />
            <stop offset="1" stopColor="#22d3ee" />
          </linearGradient>
        </defs>
        <rect width="32" height="32" rx="9" fill="#1d1b2e" />
        <path d="M9 21c4-1 6-4 7-8 1 4 3 7 7 8-4 0-6 2-7 4-1-2-3-4-7-4z" fill={`url(#${id})`} />
      </svg>
      <div className="leading-tight">
        <div className="text-[15px] font-semibold tracking-tight">Sylph</div>
        <div className="text-[11px] text-muted-foreground">панель управления</div>
      </div>
    </div>
  )
}
