import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { NavLink, Navigate, Outlet, useLocation, useMatches, useNavigate } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { motion } from "motion/react"
import { CreditCard, LayoutDashboard, Layers, LogOut, Menu, ScrollText, Server, Tags, Users } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { auth, setUnauthorizedHandler } from "@/lib/api"
import { useMe } from "@/lib/session"
import { cn } from "@/lib/utils"
import { Logo } from "./Logo"

const nav = [
  { to: "/", label: "Сводка", icon: LayoutDashboard, end: true },
  { to: "/users", label: "Юзеры", icon: Users },
  { to: "/payments", label: "Платежи", icon: CreditCard },
  { to: "/nodes", label: "Ноды", icon: Server },
  { to: "/plans", label: "Тарифы", icon: Tags },
  { to: "/groups", label: "Группы", icon: Layers },
  { to: "/audit", label: "Аудит", icon: ScrollText },
]

function NavItems({ onNavigate }: { onNavigate?: () => void }) {
  const ref = useRef<HTMLElement>(null)
  const { pathname } = useLocation()
  const [pill, setPill] = useState<{ y: number; h: number } | null>(null)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const measure = () => {
      const a = el.querySelector<HTMLElement>('[aria-current="page"]')
      setPill(a && a.offsetHeight > 0 ? { y: a.offsetTop, h: a.offsetHeight } : null)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [pathname])

  return (
    <nav ref={ref} className="relative flex flex-col gap-1">
      {pill && (
        <motion.span
          aria-hidden
          className="glass-pill absolute inset-x-0 top-0 rounded-[1rem]"
          initial={false}
          animate={{ y: pill.y, height: pill.h }}
          transition={{ type: "spring", bounce: 0.2, duration: 0.55 }}
        />
      )}
      {nav.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.end}
          onClick={onNavigate}
          className={({ isActive }) =>
            cn(
              "group relative flex items-center gap-3 rounded-[1rem] px-3 py-2.5 text-sm font-medium text-muted-foreground transition-colors duration-200 hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring focus-visible:outline-none",
              !isActive && "hover:bg-white/5",
              isActive && "text-foreground",
            )
          }
        >
          {({ isActive }) => (
            <>
              <item.icon
                className={cn(
                  "relative size-[1.125rem] transition-colors duration-200",
                  isActive ? "text-primary drop-shadow-[0_0_8px_oklch(0.74_0.15_287/55%)]" : "text-muted-foreground group-hover:text-foreground",
                )}
              />
              <span className="relative">{item.label}</span>
            </>
          )}
        </NavLink>
      ))}
    </nav>
  )
}

function UserBlock({ name, onLogout }: { name: string; onLogout: () => void }) {
  return (
    <div className="flex items-center gap-3 rounded-[1rem] bg-white/[0.045] p-2 pl-2.5 shadow-[inset_0_0_0_1px_oklch(1_0_0/7%)]">
      <div aria-hidden className="flex size-9 shrink-0 items-center justify-center rounded-full bg-linear-to-br from-violet-500/80 to-cyan-500/70 text-sm font-semibold text-white shadow-[inset_0_1px_0_0_oklch(1_0_0/40%)]">
        {(Array.from(name)[0] ?? "").toUpperCase()}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{name}</div>
        <div className="text-xs text-muted-foreground">админ</div>
      </div>
      <Button variant="ghost" size="icon-sm" onClick={onLogout} title="Выйти" aria-label="Выйти">
        <LogOut />
      </Button>
    </div>
  )
}

export function AppShell() {
  const me = useMe()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const matches = useMatches()
  const [open, setOpen] = useState(false)

  useEffect(() => {
    setUnauthorizedHandler(() => {
      qc.setQueryData(["me"], null)
    })
  }, [qc])

  if (me.isPending) {
    return (
      <div className="flex min-h-dvh items-center justify-center">
        <Skeleton className="h-8 w-40" />
      </div>
    )
  }
  if (!me.data) {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  }

  const logout = async () => {
    try {
      await auth.logout()
    } finally {
      qc.clear()
      navigate("/login", { replace: true })
    }
  }

  const screen = matches[matches.length - 1]?.id ?? location.pathname

  return (
    <div className="min-h-dvh">
      <aside className="glass fixed top-3 bottom-3 left-3 z-30 hidden w-64 flex-col rounded-3xl p-3 lg:flex">
        <div className="px-2 pt-2 pb-6">
          <Logo />
        </div>
        <NavItems />
        <div className="mt-auto">
          <UserBlock name={me.data.name} onLogout={logout} />
        </div>
      </aside>

      <header className="glass sticky top-2 z-30 mx-2 mt-2 flex h-14 items-center gap-2 rounded-2xl px-2 lg:hidden">
        <Button variant="ghost" size="icon" className="size-10" onClick={() => setOpen(true)} aria-label="Меню">
          <Menu className="size-5" />
        </Button>
        <Logo />
      </header>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent className="p-3">
          <SheetTitle>Меню</SheetTitle>
          <SheetDescription>Разделы панели</SheetDescription>
          <div className="px-2 pt-2 pb-6">
            <Logo />
          </div>
          <NavItems onNavigate={() => setOpen(false)} />
          <div className="mt-auto">
            <UserBlock name={me.data.name} onLogout={logout} />
          </div>
        </SheetContent>
      </Sheet>

      <main className="px-4 pt-5 pb-10 sm:px-6 lg:ml-[17.5rem] lg:px-8 lg:pt-8">
        <motion.div
          key={screen}
          className="mx-auto w-full max-w-7xl"
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.38, ease: [0.22, 1, 0.36, 1] }}
        >
          <Outlet />
        </motion.div>
      </main>
    </div>
  )
}
