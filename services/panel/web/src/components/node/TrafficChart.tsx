import { useLayoutEffect, useRef, useState, type KeyboardEvent } from "react"
import { fmtBytes, fmtShortDay } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { UpDown } from "@/lib/types"

const PLOT_H = 128
const TOP = 18
const BOTTOM = 22
const weekdays = ["вс", "пн", "вт", "ср", "чт", "пт", "сб"]

function weekday(day: string): string {
  const [y, m, d] = day.split("-").map(Number)
  return weekdays[new Date(Date.UTC(y, m - 1, d)).getUTCDay()]
}

export function niceCeil(bytes: number): number {
  if (bytes <= 0) return 1
  let unit = 1
  while (bytes / unit >= 1024) unit *= 1024
  const v = bytes / unit
  const exp = 10 ** Math.floor(Math.log10(v))
  const f = v / exp
  const step = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10
  return step * exp * unit
}

function barPath(x: number, w: number, base: number, h: number): string {
  const r = Math.min(4, w / 2, h)
  const y = base - h
  return `M${x},${base}V${y + r}Q${x},${y} ${x + r},${y}H${x + w - r}Q${x + w},${y} ${x + w},${y + r}V${base}Z`
}

export function TrafficChart({ days, highlight }: { days: (UpDown & { day: string })[]; highlight: number }) {
  const box = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [active, setActive] = useState<number | null>(null)

  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    setWidth(el.clientWidth)
    const ro = new ResizeObserver(() => setWidth(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const n = days.length
  const totals = days.map((d) => d.up + d.down)
  const top = niceCeil(Math.max(...totals, 0))
  const slot = n > 0 ? width / n : 0
  const barW = Math.max(2, Math.min(24, slot - 2, slot * 0.7))
  const base = TOP + PLOT_H
  const cur = active === null ? null : days[active]
  const curX = active === null ? 0 : Math.min(Math.max(active * slot + slot / 2, 72), Math.max(width - 72, 72))

  const onKey = (e: KeyboardEvent) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return
    e.preventDefault()
    const from = active ?? n - 1
    setActive(Math.min(n - 1, Math.max(0, from + (e.key === "ArrowLeft" ? -1 : 1))))
  }

  return (
    <div ref={box} className="relative w-full min-w-0 select-none">
      {width > 0 && (
        <svg
          width={width}
          height={TOP + PLOT_H + BOTTOM}
          className="block touch-pan-y outline-none focus-visible:rounded-lg focus-visible:ring-[3px] focus-visible:ring-ring"
          role="img"
          aria-label="Трафик ноды по дням за 30 дней"
          tabIndex={0}
          onKeyDown={onKey}
          onBlur={() => setActive(null)}
          onPointerLeave={(e) => e.pointerType === "mouse" && setActive(null)}
        >
          <line x1={0} x2={width} y1={TOP} y2={TOP} className="stroke-white/10" strokeWidth={1} />
          <line x1={0} x2={width} y1={base + 0.5} y2={base + 0.5} className="stroke-white/18" strokeWidth={1} />
          <text x={0} y={TOP - 6} className="fill-muted-foreground text-[11px] tabular-nums">
            {fmtBytes(top)}
          </text>
          {days.map((d, i) => {
            const h = totals[i] > 0 ? Math.max(2, (totals[i] / top) * PLOT_H) : 0
            const inWindow = i >= n - highlight
            const x = i * slot + (slot - barW) / 2
            return (
              h > 0 && (
                <path
                  key={d.day}
                  d={barPath(x, barW, base, h)}
                  className={cn(
                    "transition-[fill-opacity] duration-300",
                    active === i ? "fill-info" : inWindow ? "fill-info/85" : "fill-info/25",
                  )}
                />
              )
            )
          })}
          {active !== null && <line x1={active * slot + slot / 2} x2={active * slot + slot / 2} y1={TOP} y2={base} className="stroke-white/15" strokeWidth={1} />}
          {days.map((d, i) => (
            <rect
              key={d.day}
              x={i * slot}
              y={0}
              width={slot}
              height={base}
              fill="transparent"
              onPointerEnter={(e) => e.pointerType === "mouse" && setActive(i)}
              onPointerDown={() => setActive(active === i ? null : i)}
            />
          ))}
          {n > 0 && (
            <>
              <text x={0} y={base + 16} className="fill-muted-foreground text-[11px]">
                {fmtShortDay(days[0].day)}
              </text>
              <text x={width} y={base + 16} textAnchor="end" className="fill-muted-foreground text-[11px]">
                сегодня
              </text>
            </>
          )}
        </svg>
      )}
      {cur && (
        <div
          role="status"
          className="glass-strong pointer-events-none absolute top-0 z-10 -translate-x-1/2 -translate-y-full rounded-xl px-3 py-2 text-xs whitespace-nowrap shadow-lg"
          style={{ left: curX }}
        >
          <div className="text-muted-foreground">
            {weekday(cur.day)}, {fmtShortDay(cur.day)}
          </div>
          <div className="text-sm font-semibold tabular-nums">{fmtBytes(cur.up + cur.down)}</div>
          <div className="text-muted-foreground tabular-nums">
            ↓ {fmtBytes(cur.down)} · ↑ {fmtBytes(cur.up)}
          </div>
        </div>
      )}
      <table className="sr-only">
        <caption>Трафик ноды по дням</caption>
        <thead>
          <tr>
            <th>День</th>
            <th>Всего</th>
            <th>Вниз</th>
            <th>Вверх</th>
          </tr>
        </thead>
        <tbody>
          {days.map((d) => (
            <tr key={d.day}>
              <td>{fmtShortDay(d.day)}</td>
              <td>{fmtBytes(d.up + d.down)}</td>
              <td>{fmtBytes(d.down)}</td>
              <td>{fmtBytes(d.up)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
