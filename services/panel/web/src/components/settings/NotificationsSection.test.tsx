import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { Settings } from "@/lib/types"
import { NotificationsSection } from "./NotificationsSection"

const patch = vi.fn()

vi.mock("@/lib/api", () => ({ core: { patch: (...args: unknown[]) => patch(...args) } }))
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }))

const settings: Settings = {
  subs_title: "Sylph",
  subs_support_url: "",
  notify_traffic: true,
  notify_expiry: true,
  notify_trial: false,
  terms_enabled: false,
  terms_text: "",
  terms_url: "",
  terms_version: 1,
  channel_enabled: false,
  channel_chat: "",
  channel_url: "",
  channel_text: "",
  help_enabled: false,
  help_text: "",
  help_buttons: [],
  updated_at: "2026-10-23T12:00:00Z",
}

function renderSection() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <NotificationsSection settings={settings} />
    </QueryClientProvider>,
  )
}

afterEach(() => {
  cleanup()
  patch.mockReset()
})

describe("NotificationsSection", () => {
  it("shows each toggle in its saved state", () => {
    renderSection()
    expect(screen.getByRole("switch", { name: "Трафик" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByRole("switch", { name: "Срок подписки" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByRole("switch", { name: "Пробный период" }).getAttribute("aria-checked")).toBe("false")
  })

  it("asks before turning a type off and saves only that field", async () => {
    patch.mockResolvedValue({})
    renderSection()
    fireEvent.click(screen.getByRole("switch", { name: "Трафик" }))
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Выключить уведомления: трафик")
    expect(dialog.textContent).toContain("перестанут получать")
    expect(patch).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Выключить" }))
    await waitFor(() => expect(patch).toHaveBeenCalledWith("/v1/admin/settings", { notify_traffic: false }))
  })

  it("explains what turning a type on will send", async () => {
    renderSection()
    fireEvent.click(screen.getByRole("switch", { name: "Пробный период" }))
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Включить уведомления: пробный период")
    expect(dialog.textContent).toContain("за последние сутки")
    fireEvent.click(screen.getByRole("button", { name: "Отмена" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(patch).not.toHaveBeenCalled()
  })
})
