import { useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { LifeBuoy } from "lucide-react"
import { PageHeader } from "@/components/layout/PageHeader"
import { ActionDialog } from "@/components/common/ActionDialog"
import { LoadError, LoadingBlock } from "@/components/common/QueryState"
import { Section } from "@/components/common/Section"
import { GeoSourcesSection } from "@/components/routing/GeoSourcesSection"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { core } from "@/lib/api"
import { fmtDateTime } from "@/lib/format"
import { useInvalidate } from "@/lib/queries"
import type { Settings } from "@/lib/types"

const urlPattern = /^https?:\/\/[\x21-\x2e\x30-\x3e\x40-\x7e]+([/?#][\x21-\x7e]*)?$/

export function SettingsPage() {
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => core.get<Settings>("/v1/admin/settings") })
  const invalidate = useInvalidate()
  const [title, setTitle] = useState("")
  const [support, setSupport] = useState("")
  const [confirm, setConfirm] = useState(false)

  useEffect(() => {
    if (!settings.data) return
    setTitle(settings.data.subs_title)
    setSupport(settings.data.subs_support_url)
  }, [settings.data])

  if (settings.isPending) return <LoadingBlock rows={3} />
  if (settings.isError) return <LoadError error={settings.error} onRetry={() => settings.refetch()} />

  const s = settings.data
  const t = title.trim()
  const u = support.trim()
  const changed: Record<string, string> = {}
  if (t !== s.subs_title) changed.subs_title = t
  if (u !== s.subs_support_url) changed.subs_support_url = u
  const titleOk = t !== "" && t.length <= 64
  const urlOk = u === "" || (u.length <= 512 && urlPattern.test(u))
  const dirty = Object.keys(changed).length > 0

  return (
    <>
      <PageHeader title="Настройки" description="общие для всей системы" />
      <Section title="Подписка" description="как подписку видят клиенты" className="max-w-3xl">
        <form
          className="flex flex-col gap-5"
          onSubmit={(e) => {
            e.preventDefault()
            if (dirty && titleOk && urlOk) setConfirm(true)
          }}
        >
          <div className="grid grid-cols-1 gap-5 md:grid-cols-[minmax(0,1fr)_16rem]">
            <div className="flex min-w-0 flex-col gap-5">
              <div className="flex flex-col gap-2">
                <Label htmlFor="subs-title">Название</Label>
                <Input id="subs-title" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={64} aria-invalid={!titleOk} />
                <p className="text-xs text-muted-foreground">в приложениях — имя профиля подписки, на страничке подписки — заголовок</p>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="subs-support">Ссылка поддержки</Label>
                <Input id="subs-support" value={support} onChange={(e) => setSupport(e.target.value)} placeholder="https://t.me/…" inputMode="url" aria-invalid={!urlOk} />
                <p className={urlOk ? "text-xs text-muted-foreground" : "text-xs text-[oklch(0.82_0.12_22)]"}>
                  {urlOk ? "кнопка «Поддержка» в Happ / v2RayTun и на страничке подписки; пусто — кнопки нет" : "нужна ссылка вида https://… латиницей и без пробелов (кириллический домен — в punycode, xn--…)"}
                </p>
              </div>
            </div>
            <div className="flex flex-col gap-2">
              <div className="text-sm font-medium">Пример</div>
              <div className="well flex flex-col items-start gap-3 rounded-2xl p-4">
                <div className="text-xs text-muted-foreground">профиль в приложении</div>
                <div className="w-full truncate text-lg font-semibold">{t || "—"}</div>
                {urlOk && u !== "" ? (
                  <span className="glass-pill inline-flex h-8 items-center gap-1.5 rounded-full px-3 text-sm">
                    <LifeBuoy className="size-4" />
                    Поддержка
                  </span>
                ) : (
                  <span className="text-xs text-muted-foreground">без кнопки поддержки</span>
                )}
              </div>
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-xs text-muted-foreground">изменено {fmtDateTime(s.updated_at)}</span>
            <div className="flex gap-2">
              {dirty && (
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => {
                    setTitle(s.subs_title)
                    setSupport(s.subs_support_url)
                  }}
                >
                  Вернуть
                </Button>
              )}
              <Button type="submit" disabled={!dirty || !titleOk || !urlOk}>
                Сохранить
              </Button>
            </div>
          </div>
        </form>
      </Section>

      <div className="mt-4">
        <GeoSourcesSection />
      </div>

      <ActionDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Сохранить настройки подписки"
        consequences={
          <>
            <p>Приложения покажут новое название и поддержку при следующем обновлении подписки — сами они обновляют её по умолчанию раз в 12 часов, или юзер жмёт «обновить».</p>
            <p>Страничка подписки в браузере изменится сразу.</p>
          </>
        }
        confirmLabel="Сохранить"
        success="Настройки сохранены"
        onConfirm={async () => {
          await core.patch("/v1/admin/settings", changed)
          await invalidate(["settings"], ["audit"])
        }}
      />
    </>
  )
}
