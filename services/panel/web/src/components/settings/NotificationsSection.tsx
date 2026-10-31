import { useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Section } from "@/components/common/Section"
import { Switch } from "@/components/ui/switch"
import { core } from "@/lib/api"
import { useInvalidate } from "@/lib/queries"
import type { Settings } from "@/lib/types"

type NotifyKey = "notify_traffic" | "notify_expiry" | "notify_trial"

export const notifyKinds: { key: NotifyKey; title: string; hint: string }[] = [
  { key: "notify_traffic", title: "Трафик", hint: "при расходе 80, 95 и 100 % лимита пула в текущем периоде" },
  { key: "notify_expiry", title: "Срок подписки", hint: "за 3 дня, за 1 день, за 1 час и после окончания" },
  { key: "notify_trial", title: "Пробный период", hint: "за 1 день, за 1 час и после окончания" },
]

export function NotificationsSection({ settings }: { settings: Settings }) {
  const invalidate = useInvalidate()
  const [pending, setPending] = useState<{ key: NotifyKey; on: boolean }>({ key: "notify_traffic", on: false })
  const [open, setOpen] = useState(false)
  const kind = notifyKinds.find((k) => k.key === pending.key) ?? notifyKinds[0]

  return (
    <>
      <Section title="Уведомления пользователям" description="бот автоматически отправляет сообщение при достижении порога" className="max-w-3xl">
        <div className="flex flex-col gap-3">
          {notifyKinds.map((k) => (
            <div key={k.key} className="well flex items-center justify-between gap-3 rounded-xl p-3">
              <div>
                <div className="text-sm font-medium">{k.title}</div>
                <div className="text-xs text-muted-foreground">{k.hint}</div>
              </div>
              <Switch
                aria-label={k.title}
                checked={settings[k.key]}
                onCheckedChange={(on) => {
                  setPending({ key: k.key, on })
                  setOpen(true)
                }}
              />
            </div>
          ))}
          <p className="text-xs text-muted-foreground">
            Если пройдено несколько порогов, отправляется одно сообщение о самом срочном: при переходе сразу через 80 % и 95 % придёт только сообщение о 95 %. Сообщения содержат кнопки «Купить / продлить» и «Моя подписка». Пользователям, заблокировавшим бота, сообщения не отправляются.
          </p>
        </div>
      </Section>

      <ActionDialog
        open={open}
        onOpenChange={setOpen}
        title={`${pending.on ? "Включить" : "Выключить"} уведомления: ${kind.title.toLowerCase()}`}
        consequences={
          pending.on ? (
            <p>
              Бот начнёт отправлять эти уведомления в течение минуты. Пользователи, уже прошедшие порог, получат одно сообщение о самом срочном событии; об окончании подписки — только если она закончилась за последние сутки.
            </p>
          ) : (
            <p>Пользователи перестанут получать эти уведомления. После повторного включения будут отправлены только актуальные на тот момент уведомления.</p>
          )
        }
        confirmLabel={pending.on ? "Включить" : "Выключить"}
        success={pending.on ? "Уведомления включены" : "Уведомления выключены"}
        onConfirm={async () => {
          await core.patch("/v1/admin/settings", { [pending.key]: pending.on })
          await invalidate(["settings"], ["audit"])
        }}
      />
    </>
  )
}
