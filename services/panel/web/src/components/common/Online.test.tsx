import { afterEach, describe, expect, it } from "vitest"
import { cleanup, render, screen } from "@testing-library/react"
import { OnlineState } from "./Online"

afterEach(cleanup)

describe("OnlineState", () => {
  it("lists the nodes a user is on right now", () => {
    render(<OnlineState nodes={[{ id: 1, name: "Германия" }, { id: 2, name: "Финляндия" }]} lastSeen="2026-10-24T10:00:00Z" />)
    expect(screen.getByText("Германия, Финляндия")).toBeTruthy()
    expect(screen.queryByText(/был/)).toBeNull()
  })

  it("says when the user was last seen", () => {
    render(<OnlineState nodes={[]} lastSeen={new Date(Date.now() - 2 * 3600 * 1000).toISOString()} />)
    expect(screen.getByText("был 2 ч назад")).toBeTruthy()
  })

  it("tells a user who never connected", () => {
    render(<OnlineState nodes={[]} lastSeen={null} />)
    expect(screen.getByText("не заходил")).toBeTruthy()
  })
})
