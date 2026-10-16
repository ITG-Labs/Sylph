import { describe, expect, it } from "vitest"
import { parseProxyLink } from "./proxylink"

const pbk = "Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw"

describe("parseProxyLink", () => {
  it("reads a vless reality link", () => {
    const r = parseProxyLink(`vless://6F1C2A3B-4D5E-4F60-8A7B-9C0D1E2F3A4B@NL.Example.com:443?type=tcp&security=reality&sni=www.microsoft.com&fp=chrome&pbk=${pbk}&sid=AB12&flow=xtls-rprx-vision#Нидерланды%201`)
    expect(r.ok).toBe(true)
    if (!r.ok) return
    expect(r.proxy).toEqual({
      protocol: "vless",
      address: "nl.example.com",
      port: 443,
      uuid: "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b",
      security: "reality",
      sni: "www.microsoft.com",
      fingerprint: "chrome",
      public_key: pbk,
      short_id: "ab12",
      flow: "xtls-rprx-vision",
    })
    expect(r.name).toBe("Нидерланды 1")
  })

  it("reads vless over tls and refuses other transports", () => {
    const r = parseProxyLink("vless://6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b@v.example.com:8443?security=tls&sni=cdn.example.com")
    expect(r.ok && r.proxy).toMatchObject({ protocol: "vless", security: "tls", sni: "cdn.example.com", port: 8443 })
    const ws = parseProxyLink("vless://6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b@v.example.com:443?type=ws&security=tls")
    expect(ws.ok).toBe(false)
    if (!ws.ok) expect(ws.error).toContain("ws")
  })

  it("reads trojan", () => {
    const r = parseProxyLink("trojan://p%40ss@t.example.com:443?sni=t.example.com&fp=firefox#T")
    expect(r.ok && r.proxy).toEqual({ protocol: "trojan", address: "t.example.com", port: 443, password: "p@ss", security: "tls", sni: "t.example.com", fingerprint: "firefox" })
  })

  it("reads shadowsocks in both encodings", () => {
    const sip002 = parseProxyLink(`ss://${btoa("aes-256-gcm:secret")}@s.example.com:8388#SS`)
    expect(sip002.ok && sip002.proxy).toEqual({ protocol: "shadowsocks", address: "s.example.com", port: 8388, method: "aes-256-gcm", password: "secret" })
    const plain = parseProxyLink("ss://2022-blake3-aes-128-gcm:a2V5a2V5a2V5a2V5a2V5aw%3D%3D@s.example.com:8388")
    expect(plain.ok && plain.proxy).toMatchObject({ method: "2022-blake3-aes-128-gcm", password: "a2V5a2V5a2V5a2V5a2V5aw==" })
    const legacy = parseProxyLink(`ss://${btoa("chacha20-ietf-poly1305:pw@1.2.3.4:443")}#old`)
    expect(legacy.ok && legacy.proxy).toEqual({ protocol: "shadowsocks", address: "1.2.3.4", port: 443, method: "chacha20-ietf-poly1305", password: "pw" })
  })

  it("reads socks and http with and without credentials", () => {
    expect(parseProxyLink("socks5://u:p@f.example.com:1080").ok && parseProxyLink("socks5://u:p@f.example.com:1080")).toMatchObject({ proxy: { protocol: "socks", username: "u", password: "p", port: 1080 } })
    expect(parseProxyLink(`socks://${btoa("u:p")}@f.example.com:1080`)).toMatchObject({ ok: true, proxy: { username: "u", password: "p" } })
    expect(parseProxyLink("http://h.example.com:3128")).toMatchObject({ ok: true, proxy: { protocol: "http", address: "h.example.com", port: 3128 } })
    expect(parseProxyLink("https://u:p@h.example.com")).toMatchObject({ ok: true, proxy: { protocol: "http", security: "tls", port: 443, username: "u" } })
  })

  it("keeps ipv6 addresses without brackets", () => {
    const r = parseProxyLink("socks://[2001:db8::1]:1080")
    expect(r.ok && r.proxy.address).toBe("2001:db8::1")
  })

  it("explains what it cannot read", () => {
    for (const bad of ["", "vmess://abc", "vless://@host:443", "trojan://pw@host", "ss://garbage@host:1", "not a link"]) {
      const r = parseProxyLink(bad)
      expect(r.ok).toBe(false)
    }
  })
})
