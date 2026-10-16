import { expect, test, type Page } from "@playwright/test"
import { totp } from "./totp"

const admin = process.env.PANEL_ADMIN ?? ""
const password = process.env.PANEL_PASSWORD ?? ""
const secret = process.env.PANEL_TOTP_SECRET ?? ""

test.skip(!admin || !password || !secret, "PANEL_ADMIN, PANEL_PASSWORD and PANEL_TOTP_SECRET are required")
test.describe.configure({ mode: "serial" })

async function submitLogin(page: Page, code: string) {
  await page.goto("/login")
  await page.locator("#name").fill(admin)
  await page.locator("#password").fill(password)
  await page.locator("#code").fill(code)
  await page.getByRole("button", { name: "Войти" }).click()
}

async function login(page: Page) {
  await submitLogin(page, totp(secret))
  const ok = page.getByRole("heading", { name: "Сводка" })
  const refused = page.getByRole("alert")
  await expect(ok.or(refused)).toBeVisible()
  if (await refused.isVisible()) {
    await page.waitForTimeout(30_000 - (Date.now() % 30_000) + 500)
    await submitLogin(page, totp(secret))
  }
  await expect(ok).toBeVisible()
}

let shared: Page

test.beforeAll(async ({ browser }) => {
  test.setTimeout(75_000)
  shared = await (await browser.newContext()).newPage()
  await login(shared)
})

test.afterAll(async () => {
  await shared?.context().close()
})

test("unauthenticated visitor lands on login", async ({ page }) => {
  await page.goto("/users")
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole("heading", { name: "Вход" })).toBeVisible()
})

test("wrong code is refused with a human message", async ({ page }) => {
  await submitLogin(page, "000000")
  await expect(page.getByRole("alert")).toContainText(/Неверное имя, пароль или код|Слишком много попыток/)
})

test("every screen renders without console errors under the real CSP", async () => {
  const errors: string[] = []
  shared.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text())
  })
  shared.on("pageerror", (e) => errors.push(e.message))
  for (const [path, heading] of [
    ["/", "Сводка"],
    ["/users", "Юзеры"],
    ["/payments", "Платежи"],
    ["/nodes", "Ноды"],
    ["/routing", "Маршрутизация"],
    ["/plans", "Тарифы"],
    ["/groups", "Группы локаций"],
    ["/audit", "Аудит"],
    ["/settings", "Настройки"],
  ]) {
    await shared.goto(path)
    await expect(shared.getByRole("heading", { name: heading, exact: true })).toBeVisible()
  }
  await shared.goto("/users")
  const first = shared.locator("table tbody tr").first()
  if (await first.count()) {
    await first.click()
    await expect(shared.getByText("Ссылка подписки")).toBeVisible()
    await shared.getByRole("button", { name: "Ещё действия" }).click()
    await expect(shared.getByRole("menuitem", { name: "Сменить ссылку подписки" })).toBeVisible()
    await shared.keyboard.press("Escape")
  }
  expect(errors).toEqual([])
})

test("a group is created, renamed and deleted while empty", async () => {
  const name = `e2e-${Date.now() % 1_000_000}`
  await shared.goto("/groups")
  await shared.getByRole("button", { name: "Новая группа" }).click()
  await shared.locator("#gname").fill(name)
  await shared.getByRole("dialog").getByRole("button", { name: "Создать" }).click()
  await shared.getByRole("link", { name: new RegExp(name) }).click()
  await expect(shared.getByRole("heading", { name, exact: true })).toBeVisible()
  await shared.getByRole("button", { name: "Переименовать" }).click()
  await shared.locator("#gname").fill(`${name}-x`)
  await shared.getByRole("dialog").getByRole("button", { name: "Сохранить" }).click()
  await expect(shared.getByRole("heading", { name: `${name}-x`, exact: true })).toBeVisible()
  await shared.getByRole("button", { name: "Удалить", exact: true }).click()
  await expect(shared.getByRole("dialog")).toContainText("Группа пустая")
  await shared.getByRole("dialog").getByRole("button", { name: "Удалить", exact: true }).click()
  await expect(shared).toHaveURL(/\/groups$/)
  await expect(shared.getByText(`${name}-x`)).toHaveCount(0)
})

test("a group with members cannot be deleted", async () => {
  const groups = await shared.evaluate(async () => (await (await fetch("/api/core/v1/admin/groups")).json()) as { id: number }[])
  const nodes = await shared.evaluate(async () => (await (await fetch("/api/core/v1/admin/nodes")).json()) as { group_id: number | null }[])
  const busy = groups.find((g) => nodes.some((n) => n.group_id === g.id))
  test.skip(!busy, "no group with nodes on this stand")
  await shared.goto(`/groups/${busy!.id}`)
  await shared.getByRole("button", { name: "Удалить", exact: true }).click()
  await expect(shared.getByRole("dialog")).toContainText("Удалить можно только пустую группу")
  await expect(shared.getByRole("dialog").getByRole("button", { name: "Удалить" })).toBeDisabled()
  await shared.keyboard.press("Escape")
})

test("a node card shows its traffic", async () => {
  const nodes = await shared.evaluate(async () => (await (await fetch("/api/core/v1/admin/nodes")).json()) as { id: number }[])
  test.skip(nodes.length === 0, "no nodes on this stand")
  await shared.goto(`/nodes/${nodes[0].id}`)
  await expect(shared.getByText("вверх + вниз, сутки по Москве")).toBeVisible()
  const chart = shared.getByRole("img", { name: "Трафик ноды по дням за 30 дней" })
  await expect(chart.or(shared.getByText("За 30 дней через ноду не прошло ни байта"))).toBeVisible()
  if (await chart.isVisible()) {
    await shared.getByRole("tab", { name: "7 дней" }).click()
    await expect(shared.getByText("за 7 дней")).toBeVisible()
  }
})

test("a routing profile is built, saved, assigned to a node and removed", async () => {
  const name = `e2e-${Date.now() % 1_000_000}`
  await shared.goto("/routing")
  await shared.getByRole("button", { name: "Новый профиль" }).click()
  await shared.locator("#pf-name").fill(name)
  await shared.getByRole("dialog").getByRole("button", { name: "Создать" }).click()
  await expect(shared.getByRole("heading", { name, exact: true })).toBeVisible()
  const url = shared.url()

  await shared.getByRole("button", { name: "Выход", exact: true }).click()
  await shared.getByLabel("Ссылка прокси").fill("socks://u:p@proxy.example.com:1080#E2E")
  await shared.getByRole("button", { name: "Разобрать" }).click()
  await expect(shared.locator("#ob-tag")).toHaveValue("e2e")
  await shared.getByRole("dialog").getByRole("button", { name: "Добавить" }).click()

  await shared.getByRole("button", { name: "Шаблон" }).click()
  await shared.getByRole("menuitem", { name: /Блок торрентов/ }).click()
  await shared.getByRole("button", { name: "Правило", exact: true }).click()
  await shared.getByLabel("Домены").fill("example.org")
  await shared.getByRole("dialog").getByRole("combobox").last().click()
  await shared.getByRole("option", { name: /^e2e/ }).click()
  await shared.getByRole("dialog").getByRole("button", { name: "Добавить" }).click()

  await expect(shared.getByText("Есть несохранённые изменения")).toBeVisible()
  await shared.getByRole("button", { name: "Сохранить", exact: true }).click()
  await shared.getByRole("dialog").getByRole("button", { name: "Сохранить" }).click()
  await expect(shared.getByText("Есть несохранённые изменения")).toHaveCount(0)
  await shared.reload()
  await expect(shared.getByText("domain:example.org")).toBeVisible()
  await expect(shared.getByText("секрет сохранён")).toBeVisible()
  await shared.getByRole("tab", { name: "JSON" }).click()
  await expect(shared.getByText('"sylph-guard"')).toBeVisible()
  await expect(shared.getByText('"pass": "***"')).toBeVisible()

  const nodes = await shared.evaluate(async () => (await (await fetch("/api/core/v1/admin/nodes")).json()) as { id: number; profile_id: number | null }[])
  const free = nodes.find((n) => n.profile_id === null)
  if (free) {
    await shared.goto(`/nodes/${free.id}`)
    await shared.getByRole("combobox", { name: "Профиль маршрутизации" }).click()
    await shared.getByRole("option", { name }).click()
    await expect(shared.getByRole("dialog")).toContainText("перезапустит xray")
    await shared.getByRole("dialog").getByRole("button", { name: "Назначить" }).click()
    await expect(shared.getByRole("link", { name: "открыть профиль" })).toBeVisible()
    await shared.goto(url)
    await shared.getByRole("button", { name: "Удалить", exact: true }).click()
    await expect(shared.getByRole("dialog").getByRole("button", { name: "Удалить" })).toBeDisabled()
    await shared.keyboard.press("Escape")
    await shared.goto(`/nodes/${free.id}`)
    await shared.getByRole("combobox", { name: "Профиль маршрутизации" }).click()
    await shared.getByRole("option", { name: /без профиля/ }).click()
    await shared.getByRole("dialog").getByRole("button", { name: "Снять" }).click()
    await expect(shared.getByRole("link", { name: "открыть профиль" })).toHaveCount(0)
  }

  await shared.goto(url)
  await shared.getByRole("button", { name: "Удалить", exact: true }).click()
  await shared.getByRole("dialog").getByRole("button", { name: "Удалить", exact: true }).click()
  await expect(shared).toHaveURL(/\/routing$/)
  await expect(shared.getByText(name)).toHaveCount(0)
})

test("users are found by nick", async () => {
  const page = await shared.evaluate(async () => (await (await fetch("/api/core/v1/admin/users?limit=100")).json()) as { users: { id: number; telegram_username: string | null }[] })
  const withNick = page.users.find((u) => u.telegram_username)
  test.skip(!withNick, "no user with a nick on this stand")
  await shared.goto(`/users?q=${encodeURIComponent("@" + withNick!.telegram_username!.slice(0, 3))}`)
  await expect(shared.locator("table").getByText(`@${withNick!.telegram_username}`)).toBeVisible()
})

test("subscription settings are saved and restored", async () => {
  const before = await shared.evaluate(async () => (await (await fetch("/api/core/v1/admin/settings")).json()) as { subs_title: string; subs_support_url: string })
  const save = async (title: string, support: string) => {
    await shared.goto("/settings")
    await shared.locator("#subs-title").fill(title)
    await shared.locator("#subs-support").fill(support)
    const button = shared.getByRole("button", { name: "Сохранить" })
    if (await button.isDisabled()) return
    await button.click()
    await shared.getByRole("dialog").getByRole("button", { name: "Сохранить" }).click()
    await expect(shared.getByRole("dialog")).toHaveCount(0)
  }
  await shared.goto("/settings")
  await shared.locator("#subs-support").fill("t.me/no-scheme")
  await expect(shared.getByText("нужна ссылка вида https://… латиницей и без пробелов")).toBeVisible()
  await shared.locator("#subs-support").fill("https://пример.рф/помощь")
  await expect(shared.getByText("нужна ссылка вида https://… латиницей и без пробелов")).toBeVisible()
  await save("e2e title", "https://t.me/e2e_help")
  const saved = await shared.evaluate(async () => (await (await fetch("/api/core/v1/admin/settings")).json()) as { subs_title: string; subs_support_url: string })
  expect([saved.subs_title, saved.subs_support_url]).toEqual(["e2e title", "https://t.me/e2e_help"])
  await save(before.subs_title, before.subs_support_url)
})

test("proxy refuses requests without the panel header and routes outside the allowlist", async () => {
  const res = await shared.evaluate(async () => {
    const noHeader = await fetch("/api/core/v1/admin/users/1/ban", { method: "POST", body: "{}" })
    const outside = await fetch("/api/core/v1/sync/users", { method: "POST", headers: { "X-Sylph-Panel": "1" }, body: "{}" })
    return [noHeader.status, outside.status]
  })
  expect(res).toEqual([403, 404])
})

test("logout ends the session", async () => {
  await shared.goto("/")
  await shared.getByRole("button", { name: "Выйти" }).click()
  await expect(shared).toHaveURL(/\/login$/)
  const status = await shared.evaluate(async () => (await fetch("/auth/me")).status)
  expect(status).toBe(401)
})
