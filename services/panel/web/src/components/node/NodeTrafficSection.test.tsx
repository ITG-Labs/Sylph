import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { afterEach, expect, it, vi } from "vitest"
import { core } from "@/lib/api"
import { NodeTrafficSection } from "./NodeTrafficSection"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it("keeps a clicked day selected, switches by keyboard, and restores the period", async () => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} })
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(300)
  const daily = [
    { day: "2026-11-25", up: 0, down: 0 },
    { day: "2026-11-26", up: 5, down: 10 },
    { day: "2026-11-27", up: 20, down: 30 },
  ]
  const user = { user_id: 1, telegram_id: 100, telegram_username: null, telegram_name: null, bytes: 15 }
  const stats = {
    daily, today: daily[2], days_7: { up: 25, down: 40 }, days_30: { up: 25, down: 40 },
    inbounds: [], top: { today: [], days_7: [], days_30: [] },
  }
  const get = vi.spyOn(core, "get").mockImplementation(async (path) => {
    if (path === "/v1/admin/nodes/42/stats") return stats
    if (path.endsWith("day=2026-11-26")) return [user]
    if (path.endsWith("day=2026-11-25")) return []
    throw new Error(path)
  })
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <NodeTrafficSection nodeId={42} inbounds={[]} />
      </QueryClientProvider>
    </MemoryRouter>,
  )
  const chart = await screen.findByRole("group", { name: /Трафик по дням/ })
  fireEvent.click(chart.querySelectorAll("rect")[1])
  fireEvent.pointerLeave(chart, { pointerType: "mouse" })
  const userLink = await screen.findByRole("link", { name: /100/ })
  expect(within(userLink).getByText("15 Б")).toBeTruthy()
  expect(screen.getByText("За 26 ноя")).toBeTruthy()
  fireEvent.keyDown(chart, { key: "ArrowLeft" })
  fireEvent.keyDown(chart, { key: "Enter" })
  await screen.findByText("Нет данных")
  expect(screen.getByText("За 25 ноя")).toBeTruthy()
  fireEvent.mouseDown(screen.getByRole("tab", { name: "7 дней" }), { button: 0 })
  expect(await screen.findByText("за 7 дней")).toBeTruthy()
  expect(get).toHaveBeenCalledWith("/v1/admin/nodes/42/stats/top?day=2026-11-26")
})
