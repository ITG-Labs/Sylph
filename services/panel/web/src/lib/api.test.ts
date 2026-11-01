import { afterEach, describe, expect, it, vi } from "vitest"
import { core } from "./api"
import { ApiError } from "./errors"

function reply(status: number, body: string, requestId = "rid-1") {
  return vi.fn(async () => new Response(body, { status, headers: { "X-Request-Id": requestId } }))
}

async function caught(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p
  } catch (e) {
    if (e instanceof ApiError) return e
    throw e
  }
  throw new Error("request did not fail")
}

describe("core client", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("returns parsed JSON and null for an empty body", async () => {
    vi.stubGlobal("fetch", reply(200, `{"ok":true}`))
    await expect(core.get("/v1/admin/overview")).resolves.toEqual({ ok: true })
    vi.stubGlobal("fetch", reply(200, ""))
    await expect(core.get("/v1/admin/overview")).resolves.toBeNull()
  })

  it("fails loudly when a successful reply is not JSON", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {})
    vi.stubGlobal("fetch", reply(200, "<!doctype html><title>proxy</title>"))
    const err = await caught(core.get("/v1/admin/overview"))
    expect(err.code).toBe("bad_reply")
    expect(err.status).toBe(200)
    expect(err.requestId).toBe("rid-1")
    expect(console.error).toHaveBeenCalled()
  })

  it("keeps the request id of core failures", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {})
    vi.stubGlobal("fetch", reply(500, `{"code":"internal","message":"boom"}`, "rid-500"))
    const err = await caught(core.post("/v1/admin/users/1/ban", {}))
    expect(err.status).toBe(500)
    expect(err.code).toBe("internal")
    expect(err.requestId).toBe("rid-500")
    expect(console.error).toHaveBeenCalled()
  })

  it("does not show raw non-JSON error pages", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {})
    vi.stubGlobal("fetch", reply(502, "<html>Bad Gateway</html>", ""))
    const err = await caught(core.get("/v1/admin/overview"))
    expect(err.code).toBe("bad_reply")
    expect(err.message).not.toContain("<html>")
  })

  it("reports network failures", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {})
    vi.stubGlobal("fetch", vi.fn(async () => Promise.reject(new TypeError("Failed to fetch"))))
    const err = await caught(core.get("/v1/admin/overview"))
    expect(err.status).toBe(0)
    expect(err.code).toBe("network")
    expect(console.error).toHaveBeenCalled()
  })
})
