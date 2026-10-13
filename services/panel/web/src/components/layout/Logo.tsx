import { useId } from "react"
import { cn } from "@/lib/utils"

export function LogoMark({ className }: { className?: string }) {
  const id = useId()
  return (
    <svg viewBox="0 0 32 32" className={cn("size-8 shrink-0", className)} aria-hidden>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#8b5cf6" />
          <stop offset="1" stopColor="#06b6d4" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="9" fill={`url(#${id})`} />
      <g fill="none" stroke="#fff" strokeWidth="2.3" strokeLinecap="round" strokeLinejoin="round">
        <path d="M7 12.5h11.5a3.2 3.2 0 1 0-3.2-3.2" />
        <path d="M7 17h15.5a3.4 3.4 0 1 1-3.4 3.4" opacity="0.92" />
        <path d="M7 21.5h6" opacity="0.75" />
      </g>
    </svg>
  )
}

export function Logo() {
  return (
    <div className="flex items-center gap-2.5">
      <LogoMark />
      <div className="leading-tight">
        <div className="text-[15px] font-semibold tracking-tight">Sylph</div>
        <div className="text-[11px] text-muted-foreground">панель управления</div>
      </div>
    </div>
  )
}
