import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { Settings } from "@/lib/types"
import { BotSection, buttonLink, channelLink, chatPattern, httpsLink } from "./BotSection"

const patch = vi.fn()
const post = vi.fn()

vi.mock("@/lib/api", () => ({ core: { patch: (...args: unknown[]) => patch(...args), post: (...args: unknown[]) => post(...args) } }))
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }))

const base: Settings = {
  subs_title: "Sylph",
  subs_support_url: "",
  notify_traffic: true,
  notify_expiry: true,
  notify_trial: true,
  terms_enabled: false,
  terms_text: "",
  terms_url: "",
  terms_version: 3,
  channel_enabled: false,
  channel_chat: "",
  channel_url: "",
  channel_text: "",
  help_enabled: true,
  help_text: "Пиши нам",
  help_buttons: [{ label: "Поддержка", url: "https://t.me/help" }],
  updated_at: "2026-10-24T12:00:00Z",
}

function renderSection(s: Partial<Settings> = {}) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <BotSection settings={{ ...base, ...s }} />
    </QueryClientProvider>,
  )
}

function saveButtons() {
  return screen.getAllByRole("button", { name: "Сохранить" })
}

afterEach(() => {
  cleanup()
  patch.mockReset()
  post.mockReset()
})

describe("patterns", () => {
  it("knows channels and links", () => {
    expect(chatPattern.test("@nora_vpn")).toBe(true)
    expect(chatPattern.test("-1001234567890")).toBe(true)
    expect(chatPattern.test("nora_vpn")).toBe(false)
    expect(chatPattern.test("@abc")).toBe(false)
    expect(channelLink("https://t.me/+AbC")).toBe(true)
    expect(channelLink("https://example.com/x")).toBe(false)
    expect(buttonLink("tg://resolve?domain=x")).toBe(true)
    expect(buttonLink("javascript:alert(1)")).toBe(false)
    expect(httpsLink("https://telegra.ph/x")).toBe(true)
    expect(httpsLink("https://x.example:99999/")).toBe(false)
    expect(httpsLink("https://localhost/x")).toBe(false)
    expect(httpsLink("https://1.2.3.4/x")).toBe(false)
    expect(httpsLink("https://user@telegra.ph/x")).toBe(false)
  })
})

describe("BotSection", () => {
  it("turns the terms on with a link and saves only what changed", async () => {
    patch.mockResolvedValue({})
    renderSection()
    fireEvent.click(screen.getByRole("switch", { name: "Пользовательское соглашение" }))
    expect(screen.getByText("без ссылки соглашение не включить")).toBeTruthy()
    expect((saveButtons()[0] as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(screen.getByLabelText("Ссылка на полный текст"), { target: { value: " https://telegra.ph/terms " } })
    fireEvent.click(saveButtons()[0])
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("версию 3")
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }))
    await waitFor(() => expect(patch).toHaveBeenCalledWith("/v1/admin/settings", { terms_enabled: true, terms_url: "https://telegra.ph/terms" }))
  })

  it("asks before bumping the terms version", async () => {
    post.mockResolvedValue({})
    renderSection({ terms_enabled: true, terms_url: "https://telegra.ph/terms" })
    fireEvent.click(screen.getByRole("button", { name: /Попросить принять заново/ }))
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("станет 4")
    expect(dialog.textContent).toContain("уже принимал")
    fireEvent.click(screen.getByRole("button", { name: "Поднять версию" }))
    await waitFor(() => expect(post).toHaveBeenCalledWith("/v1/admin/settings/terms/bump", {}))
  })

  it("checks the channel fields", () => {
    renderSection()
    fireEvent.click(screen.getByRole("switch", { name: "Подписка на канал" }))
    fireEvent.change(screen.getByLabelText("Канал"), { target: { value: "nora" } })
    expect(screen.getByText(/@имя публичного канала/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText("Канал"), { target: { value: "-1001234567890" } })
    expect(screen.getByText("у канала без @имени нужна ссылка-приглашение")).toBeTruthy()
    fireEvent.change(screen.getByLabelText("Ссылка «Подписаться»"), { target: { value: "https://t.me/+Invite" } })
    expect((saveButtons()[1] as HTMLButtonElement).disabled).toBe(false)
  })

  it("warns that the channel gate covers paying users too", async () => {
    renderSection({ channel_chat: "@nora_vpn", channel_url: "https://t.me/nora_vpn" })
    fireEvent.click(screen.getByRole("switch", { name: "Подписка на канал" }))
    fireEvent.click(saveButtons()[1])
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("и платящих")
    expect(dialog.textContent).toContain("пускает всех")
  })

  it("edits, reorders and removes help buttons", async () => {
    patch.mockResolvedValue({})
    renderSection()
    fireEvent.click(screen.getByRole("button", { name: "Добавить кнопку" }))
    fireEvent.change(screen.getByLabelText("Подпись кнопки 2"), { target: { value: " Инструкции " } })
    fireEvent.change(screen.getByLabelText("Ссылка кнопки 2"), { target: { value: "ftp://x" } })
    expect(screen.getByText(/ссылка кнопки/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText("Ссылка кнопки 2"), { target: { value: "https://telegra.ph/how" } })
    fireEvent.click(screen.getAllByRole("button", { name: "Выше" })[1])
    fireEvent.click(saveButtons()[2])
    await screen.findByRole("dialog")
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }))
    await waitFor(() =>
      expect(patch).toHaveBeenCalledWith("/v1/admin/settings", {
        help_buttons: [
          { label: "Инструкции", url: "https://telegra.ph/how" },
          { label: "Поддержка", url: "https://t.me/help" },
        ],
      }),
    )
  })

  it("does not allow more than six help buttons", () => {
    renderSection({ help_buttons: Array.from({ length: 6 }, (_, i) => ({ label: `К${i}`, url: "https://t.me/x" })) })
    expect((screen.getByRole("button", { name: "Добавить кнопку" }) as HTMLButtonElement).disabled).toBe(true)
  })
})
