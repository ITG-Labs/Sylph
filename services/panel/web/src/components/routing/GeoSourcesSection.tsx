import { useEffect, useState } from "react"
import { Link } from "react-router"
import { Pencil, Plus, RefreshCw, Trash2 } from "lucide-react"
import { ActionDialog } from "@/components/common/ActionDialog"
import { Empty, Section } from "@/components/common/Section"
import { GeoStatusList } from "@/components/node/NodeRoutingSection"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { core } from "@/lib/api"
import { ApiError, describeError } from "@/lib/errors"
import { useGeoSources, useInvalidate, useNodes } from "@/lib/queries"
import { sourcePattern, translateRouting, type GeoSource } from "@/lib/routing"
import { Field } from "./parts"

const reserved = ["full", "domain", "keyword", "regexp", "geosite", "geoip", "ext", "ext-ip", "ext-domain", "ext-site", "dotless"]
const labels: Record<string, string> = { name: "Имя", kind: "Тип", url: "Ссылка", sha256_url: "SHA-256" }

function geoError(err: unknown): string {
  if (err instanceof ApiError && labels[err.field]) {
    if (err.status === 409 && /already exists/.test(err.message)) return "Источник с таким именем уже есть"
    if (/must be a public https URL/.test(err.message)) return `${labels[err.field]}: нужна публичная ссылка https`
    if (/not a built-in prefix/.test(err.message)) return "Имя: a-z, 0-9 и дефис, до 32 знаков, не встроенный префикс"
    return `${labels[err.field]}: ${translateRouting(err.message)}`
  }
  return describeError(err)
}

const httpsOk = (u: string) => /^https:\/\/[^\s/?#]+\.[^\s/?#]+([/?#]\S*)?$/.test(u)

function SourceDialog({ open, onOpenChange, source, onDone }: { open: boolean; onOpenChange: (v: boolean) => void; source?: GeoSource; onDone: () => Promise<unknown> }) {
  const [name, setName] = useState("")
  const [kind, setKind] = useState<"domain" | "ip">("domain")
  const [url, setUrl] = useState("")
  const [sum, setSum] = useState("")

  useEffect(() => {
    if (!open) return
    setName(source?.name ?? "")
    setKind(source?.kind ?? "domain")
    setUrl(source?.url ?? "")
    setSum(source?.sha256_url ?? "")
  }, [open, source])

  const n = name.trim()
  const nameErr = n && (!sourcePattern.test(n) || reserved.includes(n)) ? "a-z, 0-9 и дефис, до 32 знаков; не geosite, geoip, ext и т. п." : null
  const urlErr = url.trim() && !httpsOk(url.trim()) ? "нужна ссылка https://…" : null
  const sumErr = sum.trim() && !httpsOk(sum.trim()) ? "нужна ссылка https://…" : null
  const ok = n !== "" && !nameErr && url.trim() !== "" && !urlErr && !sumErr

  return (
    <ActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={source ? `Источник ${source.name}` : "Новый источник geo-базы"}
      description="файл .dat в формате xray; ноды качают его сами, если на источник ссылается профиль ноды"
      consequences={source ? <p>Ноды, чьи профили ссылаются на источник, скачают базу по новой ссылке; где файл изменится — xray перезапустится.</p> : undefined}
      confirmLabel={source ? "Сохранить" : "Добавить"}
      canConfirm={ok}
      success={source ? "Источник сохранён" : "Источник добавлен"}
      onConfirm={async () => {
        const body = { name: n, kind, url: url.trim(), sha256_url: sum.trim() }
        try {
          if (source) await core.patch(`/v1/admin/geo-sources/${source.id}`, body)
          else await core.post("/v1/admin/geo-sources", body)
        } catch (err) {
          throw new Error(geoError(err))
        }
        await onDone()
      }}
    >
      <div className="grid gap-4 sm:grid-cols-[1fr_10rem]">
        <Field id="geo-name" label="Имя" error={nameErr} hint={n ? `в правилах — ${n}:тег` : "по нему правила ссылаются на базу: имя:тег"}>
          <Input id="geo-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="ru-blocked" className="font-mono" />
        </Field>
        <Field label="Что внутри">
          <Select value={kind} onValueChange={(v) => setKind(v as "domain" | "ip")}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="domain">домены</SelectItem>
              <SelectItem value="ip">IP</SelectItem>
            </SelectContent>
          </Select>
        </Field>
      </div>
      <Field id="geo-url" label="Ссылка на файл" error={urlErr} hint="только https, до 200 МБ">
        <Input id="geo-url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://github.com/…/releases/latest/download/geosite.dat" inputMode="url" className="font-mono text-xs" />
      </Field>
      <Field id="geo-sum" label="Ссылка на SHA-256" error={sumErr} hint="необязательно; файл формата sha256sum — нода сверит скачанное">
        <Input id="geo-sum" value={sum} onChange={(e) => setSum(e.target.value)} placeholder="https://…/geosite.dat.sha256sum" inputMode="url" className="font-mono text-xs" />
      </Field>
    </ActionDialog>
  )
}

export function GeoSourcesSection() {
  const sources = useGeoSources()
  const nodes = useNodes()
  const invalidate = useInvalidate()
  const [edit, setEdit] = useState<GeoSource | "new" | null>(null)
  const [del, setDel] = useState<GeoSource | null>(null)
  const [refresh, setRefresh] = useState(false)
  const reporting = (nodes.data ?? []).filter((n) => n.geo_status && Object.keys(n.geo_status).length > 0)
  const done = () => invalidate(["geo-sources"], ["profiles"], ["audit"])

  return (
    <Section
      title="Geo-базы"
      description="свои списки доменов и IP для правил маршрутизации, сверх встроенных geosite: и geoip: из образа xray"
      className="max-w-3xl"
      action={
        <Button size="sm" onClick={() => setEdit("new")}>
          <Plus />
          Источник
        </Button>
      }
    >
      <div className="flex flex-col gap-5">
        {!sources.data?.length ? (
          <Empty>Своих источников нет — правила могут брать только встроенные geosite: и geoip:</Empty>
        ) : (
          <div className="flex flex-col gap-2">
            {sources.data.map((s) => (
              <div key={s.id} className="well flex items-start gap-2 rounded-xl p-3">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono text-sm font-medium">{s.name}</span>
                    <Badge variant="secondary">{s.kind === "domain" ? "домены" : "IP"}</Badge>
                    {s.sha256_url && <Badge variant="outline">SHA-256</Badge>}
                  </div>
                  <div className="mt-1 truncate font-mono text-xs text-muted-foreground" title={s.url}>
                    {s.url}
                  </div>
                </div>
                <Button variant="ghost" size="icon-sm" aria-label="Изменить источник" onClick={() => setEdit(s)}>
                  <Pencil />
                </Button>
                <Button variant="ghost" size="icon-sm" aria-label="Удалить источник" onClick={() => setDel(s)}>
                  <Trash2 />
                </Button>
              </div>
            ))}
            <div className="flex flex-wrap items-center justify-between gap-2 pt-1">
              <span className="text-xs text-muted-foreground">ноды качают базы сами, только по кнопке и при смене ссылки</span>
              <Button size="sm" variant="secondary" onClick={() => setRefresh(true)}>
                <RefreshCw />
                Обновить на всех нодах
              </Button>
            </div>
          </div>
        )}
        {reporting.length > 0 && (
          <div className="flex flex-col gap-3">
            <div className="text-sm font-medium">На нодах</div>
            {reporting.map((n) => (
              <div key={n.id} className="flex flex-col gap-1.5">
                <Link to={`/nodes/${n.id}`} className="w-fit text-sm text-primary hover:underline">
                  {n.name}
                </Link>
                <GeoStatusList status={n.geo_status} />
              </div>
            ))}
          </div>
        )}
      </div>

      <SourceDialog open={edit !== null} onOpenChange={(v) => !v && setEdit(null)} source={edit === "new" || edit === null ? undefined : edit} onDone={done} />
      <ActionDialog
        open={del !== null}
        onOpenChange={(v) => !v && setDel(null)}
        title={`Удалить источник ${del?.name ?? ""}`}
        consequences={<p>Удалить можно, только если на источник не ссылается ни один профиль. Файл на нодах останется, но больше не обновится.</p>}
        destructive
        confirmLabel="Удалить"
        success="Источник удалён"
        onConfirm={async () => {
          try {
            await core.del(`/v1/admin/geo-sources/${del!.id}`)
          } catch (err) {
            throw new Error(geoError(err))
          }
          await done()
        }}
      />
      <ActionDialog
        open={refresh}
        onOpenChange={setRefresh}
        title="Обновить geo-базы на всех нодах"
        consequences={
          <>
            <p>Каждая нода скачает заново базы, на которые ссылается её профиль.</p>
            <p>Где файл изменился — xray перезапустится, открытые соединения на этой ноде оборвутся. Где не изменился — ничего не произойдёт.</p>
          </>
        }
        confirmLabel="Обновить"
        success="Ноды скачают базы при следующем синке"
        onConfirm={async () => {
          await core.post("/v1/admin/geo-sources/refresh")
          await invalidate(["nodes"], ["audit"])
        }}
      />
    </Section>
  )
}
