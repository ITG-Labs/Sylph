import { describe, expect, it } from "vitest"
import { agentKnowsXHTTP, certLine, clientEndpoint, extraToForm, formToExtra, kindOf, olderThan } from "./inbounds"
import type { InboundTLS } from "./types"

const ownerExtra = {
  noSSEHeader: true,
  xPaddingObfsMode: true,
  xPaddingMethod: "tokenish",
  xPaddingPlacement: "cookie",
  xPaddingKey: "_ym_d",
  seqPlacement: "query",
  seqKey: "part",
  uplinkDataPlacement: "body",
  sessionIDPlacement: "query",
  sessionIDKey: "sid",
}

const tls = (t: Partial<InboundTLS>): InboundTLS => ({ domain: "o.example.ru", decoy: "", address: "", port: 443, sni: "", host: "", alpn: null, fingerprint: "chrome", ...t })

describe("inbound kinds", () => {
  it("follow transport and security", () => {
    expect(kindOf({ transport: "tcp", security: "reality" })).toBe("reality-tcp")
    expect(kindOf({ transport: "xhttp", security: "reality" })).toBe("reality-xhttp")
    expect(kindOf({ transport: "xhttp", security: "tls" })).toBe("tls-xhttp")
  })
})

describe("xhttp extra form", () => {
  it("round-trips the owner's settings", () => {
    expect(formToExtra(extraToForm(ownerExtra))).toEqual(ownerExtra)
  })
  it("drops empty fields so xray keeps its defaults", () => {
    const form = extraToForm({})
    expect(formToExtra(form)).toEqual({})
    form.xPaddingKey = "  "
    form.scMaxBufferedPosts = ""
    expect(formToExtra(form)).toEqual({})
  })
  it("keeps numbers, ranges, headers and xmux in xray shape", () => {
    const form = extraToForm({})
    form.scMaxBufferedPosts = "30"
    form.xPaddingBytes = "100-1000"
    form.headers = "X-App: 1\n\nbad line\nX-Two:  two "
    form["xmux.maxConnections"] = "2-4"
    form["xmux.hKeepAlivePeriod"] = "30"
    expect(formToExtra(form)).toEqual({
      scMaxBufferedPosts: 30,
      xPaddingBytes: "100-1000",
      headers: { "X-App": "1", "X-Two": "two" },
      xmux: { maxConnections: "2-4", hKeepAlivePeriod: 30 },
    })
    expect(extraToForm(formToExtra(form)).headers).toBe("X-App: 1\nX-Two: two")
  })
})

describe("client endpoint", () => {
  it("defaults to the node's domain", () => {
    expect(clientEndpoint(tls({}))).toEqual({ address: "o.example.ru", port: 443, sni: "o.example.ru", host: "o.example.ru" })
  })
  it("follows a cdn given by name", () => {
    expect(clientEndpoint(tls({ address: "x.cdn.example.net", port: 8443 }))).toEqual({ address: "x.cdn.example.net", port: 8443, sni: "x.cdn.example.net", host: "x.cdn.example.net" })
  })
  it("keeps the domain as sni for a bare ip and lets host follow sni", () => {
    expect(clientEndpoint(tls({ address: "185.31.114.248" }))).toEqual({ address: "185.31.114.248", port: 443, sni: "o.example.ru", host: "o.example.ru" })
    expect(clientEndpoint(tls({ address: "185.31.114.248", sni: "x.cdn.example.net" })).host).toBe("x.cdn.example.net")
  })
})

describe("node readiness", () => {
  it("trusts a node that never synced and a new agent", () => {
    expect(agentKnowsXHTTP({ last_sync_at: null, agent_xhttp: false })).toBe(true)
    expect(agentKnowsXHTTP({ last_sync_at: "2026-10-30T10:00:00Z", agent_xhttp: true })).toBe(true)
    expect(agentKnowsXHTTP({ last_sync_at: "2026-10-30T10:00:00Z", agent_xhttp: false })).toBe(false)
  })
  it("compares xray versions by numbers", () => {
    expect(olderThan("26.10.10", "26.10.21")).toBe(true)
    expect(olderThan("26.10.21", "26.10.21")).toBe(false)
    expect(olderThan("26.11.1", "26.10.21")).toBe(false)
    expect(olderThan("25.12.31", "26.10.21")).toBe(true)
    expect(olderThan(null, "26.10.21")).toBe(false)
  })
  it("tells whether the certificate is there", () => {
    expect(certLine({ tls_status: null }, "a.ru")).toEqual({ ok: false, text: "агент ещё не проверял" })
    expect(certLine({ tls_status: { "a.ru": { not_after: "2027-01-28T10:00:00Z" } } }, "a.ru")).toEqual({ ok: true, text: "выпущен, до 28.01.2027" })
    expect(certLine({ tls_status: { "a.ru": { error: "no certificate" } } }, "a.ru")).toEqual({ ok: false, text: "нет серта: no certificate" })
  })
})
