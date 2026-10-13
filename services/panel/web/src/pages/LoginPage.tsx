import { useState, type FormEvent } from "react"
import { Navigate, useLocation, useNavigate } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { KeyRound, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Logo } from "@/components/layout/Logo"
import { auth } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { useMe } from "@/lib/session"

export function LoginPage() {
  const me = useMe()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from ?? "/"
  const [name, setName] = useState("")
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  if (me.data) return <Navigate to={from} replace />

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      const res = await auth.login(name.trim(), password, code.trim())
      qc.setQueryData(["me"], res)
      navigate(from, { replace: true })
    } catch (err) {
      setError(describeError(err))
      setCode("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-dvh items-center justify-center px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex justify-center">
          <Logo />
        </div>
        <form onSubmit={submit} className="glass-strong flex flex-col gap-5 rounded-3xl p-6 sm:p-7">
          <div>
            <h1 className="text-2xl font-bold tracking-[-0.025em]">Вход</h1>
            <p className="mt-1 text-sm text-muted-foreground">Имя, пароль и код из аутентификатора</p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="name">Имя</Label>
            <Input id="name" autoComplete="username" value={name} onChange={(e) => setName(e.target.value)} required autoFocus />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="password">Пароль</Label>
            <Input id="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="code">Код</Label>
            <Input
              id="code"
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9]{6}"
              maxLength={6}
              placeholder="000000"
              className="text-center font-mono text-lg tracking-[0.5em]"
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
              required
            />
          </div>
          {error && (
            <div role="alert" className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}
          <Button type="submit" size="lg" disabled={busy || code.length !== 6}>
            {busy ? <Loader2 className="animate-spin" /> : <KeyRound />}
            Войти
          </Button>
        </form>
      </div>
    </div>
  )
}
