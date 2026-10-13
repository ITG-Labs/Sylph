import { useEffect, useState } from "react"
import { NavLink, Navigate, Outlet, useLocation, useNavigate } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { CreditCard, LayoutDashboard, Layers, LogOut, Menu, ScrollText, Server, Tags, UserRound, Users } from "lucide-react"
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
  return (
    <nav className="flex flex-col gap-0.5">
      {nav.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.end}
          onClick={onNavigate}
          className={({ isActive }) =>
            cn(
              "group flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-white/5 hover:text-foreground",
              isActive && "bg-primary/12 text-foreground shadow-[inset_2px_0_0_var(--primary)]",
            )
          }
        >
          {({ isActive }) => (
            <>
              <item.icon className={cn("size-4.5", isActive ? "text-primary" : "text-muted-foreground group-hover:text-foreground")} />
              {item.label}
            </>
          )}
        </NavLink>
      ))}
    </nav>
  )
}

function UserBlock({ name, onLogout }: { name: string; onLogout: () => void }) {
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-white/[0.02] p-2.5">
      <div className="flex size-8 items-center justify-center rounded-full bg-primary/15 text-primary">
        <UserRound className="size-4" />
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

  return (
    <div className="bg-glow min-h-dvh">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-64 flex-col border-r bg-sidebar/80 px-4 py-5 backdrop-blur lg:flex">
        <div className="px-1 pb-6">
          <Logo />
        </div>
        <NavItems />
        <div className="mt-auto">
          <UserBlock name={me.data.name} onLogout={logout} />
        </div>
      </aside>

      <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b bg-background/80 px-4 backdrop-blur lg:hidden">
        <Button variant="ghost" size="icon" onClick={() => setOpen(true)} aria-label="Меню">
          <Menu />
        </Button>
        <Logo />
      </header>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent className="px-4 py-5">
          <SheetTitle>Меню</SheetTitle>
          <SheetDescription>Разделы панели</SheetDescription>
          <div className="px-1 pb-6">
            <Logo />
          </div>
          <NavItems onNavigate={() => setOpen(false)} />
          <div className="mt-auto">
            <UserBlock name={me.data.name} onLogout={logout} />
          </div>
        </SheetContent>
      </Sheet>

      <main className="px-4 py-6 sm:px-6 lg:ml-64 lg:px-10 lg:py-8">
        <div className="mx-auto w-full max-w-7xl">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
