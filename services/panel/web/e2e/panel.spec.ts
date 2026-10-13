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
    ["/plans", "Тарифы"],
    ["/groups", "Группы локаций"],
    ["/audit", "Аудит"],
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
