import { useState } from "react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Section } from "@/components/common/Section"
import { Switch } from "@/components/ui/switch"
import { core } from "@/lib/api"
import { useInvalidate } from "@/lib/queries"
import type { Settings } from "@/lib/types"

type NotifyKey = "notify_traffic" | "notify_expiry" | "notify_trial"

export const notifyKinds: { key: NotifyKey; title: string; hint: string }[] = [
  { key: "notify_traffic", title: "Трафик", hint: "при 80, 95 и 100 % трафика текущего периода" },
  { key: "notify_expiry", title: "Срок подписки", hint: "за 3 дня, за 1 день, за 1 час и когда закончилась" },
  { key: "notify_trial", title: "Пробный период", hint: "за 1 день, за 1 час и когда закончился" },
]

export function NotificationsSection({ settings }: { settings: Settings }) {
  const invalidate = useInvalidate()
  const [pending, setPending] = useState<{ key: NotifyKey; on: boolean }>({ key: "notify_traffic", on: false })
  const [open, setOpen] = useState(false)
  const kind = notifyKinds.find((k) => k.key === pending.key) ?? notifyKinds[0]

  return (
    <>
      <Section title="Уведомления юзерам" description="бот сам пишет юзеру, когда подходит порог" className="max-w-3xl">
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
            Из нескольких порогов приходит самый срочный: проскочил 80 % и 95 % — придёт одно сообщение, про 95 %. У сообщений кнопки «Купить / продлить» и «Моя подписка»; тем, кто заблокировал бота, не пишем.
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
              Бот начнёт присылать эти предупреждения в течение минуты. Тем, кто уже за порогом, придёт одно сообщение — самое срочное на сейчас; о закончившейся подписке — только если она закончилась за последние сутки.
            </p>
          ) : (
            <p>Юзеры перестанут получать эти предупреждения. Включишь обратно — придут только актуальные на тот момент.</p>
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
