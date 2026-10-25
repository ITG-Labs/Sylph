import { useEffect, useState, type ReactNode } from "react"
import { ArrowDown, ArrowUp, Plus, X } from "lucide-react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Section } from "@/components/common/Section"
import { Field } from "@/components/routing/parts"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { core } from "@/lib/api"
import { useInvalidate } from "@/lib/queries"
import type { HelpButton, Settings } from "@/lib/types"

export const chatPattern = /^@[A-Za-z][A-Za-z0-9_]{4,31}$|^-100\d{5,15}$/
export const httpsLink = (u: string) => {
  const m = /^https:\/\/([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)(?::(\d{1,5}))?([/?#]\S*)?$/.exec(u)
  return m !== null && /[A-Za-z]/.test(m[1].split(".").pop() ?? "") && (m[2] === undefined || (Number(m[2]) >= 1 && Number(m[2]) <= 65535))
}
export const channelLink = (u: string) => /^https:\/\/t\.me\/\S+$/.test(u)
export const buttonLink = (u: string) => httpsLink(u) || /^tg:\/\/[a-z]+\S*$/.test(u)

const maxGate = 1000
const maxHelp = 3000
const maxButtons = 6

const markupHint = (
  <>
    можно &lt;b&gt;жирный&lt;/b&gt;, &lt;i&gt;курсив&lt;/i&gt;, &lt;u&gt;, &lt;s&gt;, &lt;a href="https://…"&gt;ссылка&lt;/a&gt;, &lt;code&gt;, &lt;blockquote&gt;, &lt;tg-spoiler&gt;; знаки &lt; и &amp; — как &amp;lt; и &amp;amp;
  </>
)

type Patch = Record<string, unknown>

function Card({ title, hint, enabled, onEnabled, children }: { title: string; hint: string; enabled: boolean; onEnabled: (v: boolean) => void; children: ReactNode }) {
  return (
    <div className="well flex flex-col gap-4 rounded-xl p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-medium">{title}</div>
          <div className="text-xs text-muted-foreground">{hint}</div>
        </div>
        <Switch aria-label={title} checked={enabled} onCheckedChange={onEnabled} />
      </div>
      {children}
    </div>
  )
}

function Actions({ dirty, ok, onReset, onSave, extra }: { dirty: boolean; ok: boolean; onReset: () => void; onSave: () => void; extra?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>{extra}</div>
      <div className="flex gap-2">
        {dirty && (
          <Button type="button" variant="ghost" onClick={onReset}>
            Вернуть
          </Button>
        )}
        <Button type="button" disabled={!dirty || !ok} onClick={onSave}>
          Сохранить
        </Button>
      </div>
    </div>
  )
}

function TermsCard({ s, save, bump }: { s: Settings; save: (title: string, patch: Patch, consequences: ReactNode) => void; bump: () => void }) {
  const [enabled, setEnabled] = useState(s.terms_enabled)
  const [text, setText] = useState(s.terms_text)
  const [url, setUrl] = useState(s.terms_url)
  useEffect(() => {
    setEnabled(s.terms_enabled)
    setText(s.terms_text)
    setUrl(s.terms_url)
  }, [s.terms_enabled, s.terms_text, s.terms_url])

  const u = url.trim()
  const patch: Patch = {}
  if (enabled !== s.terms_enabled) patch.terms_enabled = enabled
  if (text !== s.terms_text) patch.terms_text = text
  if (u !== s.terms_url) patch.terms_url = u
  const urlErr = u !== "" && !httpsLink(u) ? "нужна ссылка https://…" : enabled && u === "" ? "без ссылки соглашение не включить" : null
  const textErr = text.length > maxGate ? `не больше ${maxGate} знаков` : null

  return (
    <Card title="Пользовательское соглашение" hint="перед меню бот просит принять соглашение" enabled={enabled} onEnabled={setEnabled}>
      <Field id="terms-url" label="Ссылка на полный текст" error={urlErr} hint="telegra.ph, свой сайт — открывается кнопкой «Открыть соглашение»">
        <Input id="terms-url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://telegra.ph/…" inputMode="url" />
      </Field>
      <Field id="terms-text" label="Текст на экране" error={textErr} hint={<>пусто — стандартный текст; {markupHint}</>}>
        <Textarea id="terms-text" value={text} onChange={(e) => setText(e.target.value)} rows={3} />
      </Field>
      <Actions
        dirty={Object.keys(patch).length > 0}
        ok={!urlErr && !textErr}
        onReset={() => {
          setEnabled(s.terms_enabled)
          setText(s.terms_text)
          setUrl(s.terms_url)
        }}
        onSave={() =>
          save(
            "Сохранить соглашение",
            patch,
            enabled && !s.terms_enabled ? (
              <p>Все, кто ещё не принимал версию {s.terms_version}, увидят соглашение при следующем действии в боте — в течение минуты. VPN и предупреждения работают как раньше.</p>
            ) : !enabled && s.terms_enabled ? (
              <p>Бот перестанет спрашивать соглашение. Кто уже принял — это останется записано.</p>
            ) : (
              <p>Бот покажет новый текст и ссылку в течение минуты. Тем, кто уже принял, принимать заново не нужно — для этого есть «Попросить принять заново».</p>
            ),
          )
        }
        extra={
          <div className="flex flex-wrap items-center gap-x-2">
            <span className="text-xs text-muted-foreground">версия {s.terms_version}</span>
            <Button type="button" variant="ghost" size="sm" onClick={bump}>
              Попросить принять заново
            </Button>
          </div>
        }
      />
    </Card>
  )
}

function ChannelCard({ s, save }: { s: Settings; save: (title: string, patch: Patch, consequences: ReactNode) => void }) {
  const [enabled, setEnabled] = useState(s.channel_enabled)
  const [chat, setChat] = useState(s.channel_chat)
  const [url, setUrl] = useState(s.channel_url)
  const [text, setText] = useState(s.channel_text)
  useEffect(() => {
    setEnabled(s.channel_enabled)
    setChat(s.channel_chat)
    setUrl(s.channel_url)
    setText(s.channel_text)
  }, [s.channel_enabled, s.channel_chat, s.channel_url, s.channel_text])

  const c = chat.trim()
  const u = url.trim()
  const patch: Patch = {}
  if (enabled !== s.channel_enabled) patch.channel_enabled = enabled
  if (c !== s.channel_chat) patch.channel_chat = c
  if (u !== s.channel_url) patch.channel_url = u
  if (text !== s.channel_text) patch.channel_text = text
  const chatErr = c !== "" && !chatPattern.test(c) ? "@имя публичного канала или его ID вида -1001234567890" : enabled && c === "" ? "без канала проверку не включить" : null
  const urlErr = u !== "" && !channelLink(u) ? "нужна ссылка https://t.me/…" : enabled && u === "" && c.startsWith("-100") ? "у канала без @имени нужна ссылка-приглашение" : null
  const textErr = text.length > maxGate ? `не больше ${maxGate} знаков` : null

  return (
    <Card title="Подписка на канал" hint="пока юзер не подписан, бот показывает только «Подпишись»" enabled={enabled} onEnabled={setEnabled}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field id="channel-chat" label="Канал" error={chatErr} hint="бот должен быть админом этого канала">
          <Input id="channel-chat" value={chat} onChange={(e) => setChat(e.target.value)} placeholder="@my_channel" className="font-mono" />
        </Field>
        <Field id="channel-url" label="Ссылка «Подписаться»" error={urlErr} hint="для @имени подставится сама">
          <Input id="channel-url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://t.me/…" inputMode="url" />
        </Field>
      </div>
      <Field id="channel-text" label="Текст на экране" error={textErr} hint={<>пусто — стандартный текст; {markupHint}</>}>
        <Textarea id="channel-text" value={text} onChange={(e) => setText(e.target.value)} rows={2} />
      </Field>
      <Actions
        dirty={Object.keys(patch).length > 0}
        ok={!chatErr && !urlErr && !textErr}
        onReset={() => {
          setEnabled(s.channel_enabled)
          setChat(s.channel_chat)
          setUrl(s.channel_url)
          setText(s.channel_text)
        }}
        onSave={() =>
          save(
            "Сохранить подписку на канал",
            patch,
            enabled && !s.channel_enabled ? (
              <>
                <p>В течение минуты бот перестанет пускать в меню тех, кто не подписан на канал, — и новичков, и платящих. VPN, ссылка подписки и предупреждения работают как раньше.</p>
                <p>Если бот не админ канала или Telegram не ответил — бот пускает всех и пишет ошибку в лог.</p>
              </>
            ) : !enabled && s.channel_enabled ? (
              <p>Бот перестанет проверять подписку и пустит в меню всех.</p>
            ) : (
              <p>Бот начнёт проверять подписку на новый канал в течение минуты; уже проверенных перепроверит не позже чем через 10 минут.</p>
            ),
          )
        }
      />
    </Card>
  )
}

function HelpCard({ s, save }: { s: Settings; save: (title: string, patch: Patch, consequences: ReactNode) => void }) {
  const [enabled, setEnabled] = useState(s.help_enabled)
  const [text, setText] = useState(s.help_text)
  const [buttons, setButtons] = useState<HelpButton[]>(s.help_buttons)
  useEffect(() => {
    setEnabled(s.help_enabled)
    setText(s.help_text)
    setButtons(s.help_buttons)
  }, [s.help_enabled, s.help_text, s.help_buttons])

  const cleaned = buttons.map((b) => ({ label: b.label.trim(), url: b.url.trim() }))
  const patch: Patch = {}
  if (enabled !== s.help_enabled) patch.help_enabled = enabled
  if (text !== s.help_text) patch.help_text = text
  if (JSON.stringify(cleaned) !== JSON.stringify(s.help_buttons)) patch.help_buttons = cleaned
  const textErr = text.length > maxHelp ? `не больше ${maxHelp} знаков` : enabled && text.trim() === "" ? "без текста помощь не включить" : null
  const buttonErr = cleaned.some((b) => b.label === "" || b.label.length > 32) ? "у каждой кнопки — подпись до 32 знаков" : cleaned.some((b) => !buttonLink(b.url)) ? "ссылка кнопки — https://… или tg://…" : null
  const move = (i: number, to: number) => setButtons((list) => {
    const next = [...list]
    const [b] = next.splice(i, 1)
    next.splice(to, 0, b)
    return next
  })

  return (
    <Card title="Помощь" hint="кнопка «Помощь» в меню и на экранах соглашения и канала" enabled={enabled} onEnabled={setEnabled}>
      <Field id="help-text" label="Текст" error={textErr} hint={markupHint}>
        <Textarea id="help-text" value={text} onChange={(e) => setText(e.target.value)} rows={5} />
      </Field>
      <div className="flex flex-col gap-2">
        <div className="text-sm font-medium">Кнопки</div>
        {buttons.map((b, i) => (
          <div key={i} className="flex flex-wrap items-center gap-2 sm:flex-nowrap">
            <Input aria-label={`Подпись кнопки ${i + 1}`} value={b.label} maxLength={32} placeholder="Поддержка" className="sm:w-48" onChange={(e) => setButtons((list) => list.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))} />
            <Input aria-label={`Ссылка кнопки ${i + 1}`} value={b.url} placeholder="https://t.me/…" inputMode="url" className="min-w-0 flex-1" onChange={(e) => setButtons((list) => list.map((x, j) => (j === i ? { ...x, url: e.target.value } : x)))} />
            <div className="flex">
              <Button type="button" variant="ghost" size="icon" aria-label="Выше" disabled={i === 0} onClick={() => move(i, i - 1)}>
                <ArrowUp />
              </Button>
              <Button type="button" variant="ghost" size="icon" aria-label="Ниже" disabled={i === buttons.length - 1} onClick={() => move(i, i + 1)}>
                <ArrowDown />
              </Button>
              <Button type="button" variant="ghost" size="icon" aria-label="Убрать кнопку" onClick={() => setButtons((list) => list.filter((_, j) => j !== i))}>
                <X />
              </Button>
            </div>
          </div>
        ))}
        {buttonErr ? <p className="text-xs text-[oklch(0.82_0.12_22)]">{buttonErr}</p> : <p className="text-xs text-muted-foreground">до {maxButtons} кнопок-ссылок под текстом: поддержка, инструкции, канал</p>}
        <div>
          <Button type="button" variant="ghost" size="sm" disabled={buttons.length >= maxButtons} onClick={() => setButtons((list) => [...list, { label: "", url: "" }])}>
            <Plus />
            Добавить кнопку
          </Button>
        </div>
      </div>
      <Actions
        dirty={Object.keys(patch).length > 0}
        ok={!textErr && !buttonErr}
        onReset={() => {
          setEnabled(s.help_enabled)
          setText(s.help_text)
          setButtons(s.help_buttons)
        }}
        onSave={() =>
          save(
            "Сохранить помощь",
            patch,
            enabled && !s.help_enabled ? (
              <p>Кнопка «Помощь» появится в меню бота в течение минуты.</p>
            ) : !enabled && s.help_enabled ? (
              <p>Кнопка «Помощь» пропадёт из меню в течение минуты.</p>
            ) : (
              <p>Бот покажет новый текст и кнопки в течение минуты.</p>
            ),
          )
        }
      />
    </Card>
  )
}

export function BotSection({ settings }: { settings: Settings }) {
  const invalidate = useInvalidate()
  const [pending, setPending] = useState<{ title: string; patch: Patch; consequences: ReactNode } | null>(null)
  const [bump, setBump] = useState(false)

  return (
    <>
      <Section title="Бот" description="что бот показывает до меню и в помощи" className="max-w-3xl">
        <div className="flex flex-col gap-3">
          <TermsCard s={settings} save={(title, patch, consequences) => setPending({ title, patch, consequences })} bump={() => setBump(true)} />
          <ChannelCard s={settings} save={(title, patch, consequences) => setPending({ title, patch, consequences })} />
          <HelpCard s={settings} save={(title, patch, consequences) => setPending({ title, patch, consequences })} />
          <p className="text-xs text-muted-foreground">Порядок для юзера: соглашение → канал → меню. Код из ссылки-приглашения сработает после обоих экранов. Кнопку «Проверить оплату» экраны не перекрывают.</p>
        </div>
      </Section>

      <ActionDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={pending?.title ?? ""}
        consequences={pending?.consequences}
        confirmLabel="Сохранить"
        success="Настройки бота сохранены"
        onConfirm={async () => {
          await core.patch("/v1/admin/settings", pending?.patch ?? {})
          await invalidate(["settings"], ["audit"])
        }}
      />

      <ActionDialog
        open={bump}
        onOpenChange={setBump}
        title="Попросить принять соглашение заново"
        consequences={
          <p>
            Версия соглашения станет {settings.terms_version + 1}. {settings.terms_enabled ? "Все юзеры, включая тех, кто уже принимал, увидят соглашение при следующем действии в боте и не попадут в меню, пока не примут." : "Соглашение сейчас выключено — новую версию юзеры увидят, когда ты его включишь."}
          </p>
        }
        confirmLabel="Поднять версию"
        success="Версия соглашения поднята"
        onConfirm={async () => {
          await core.post("/v1/admin/settings/terms/bump", {})
          await invalidate(["settings"], ["audit"])
        }}
      />
    </>
  )
}
