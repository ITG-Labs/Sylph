import { createHmac } from "node:crypto"

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

function base32(s: string): Buffer {
  let bits = ""
  for (const c of s.replace(/=+$/, "").toUpperCase()) {
    const v = alphabet.indexOf(c)
    if (v < 0) throw new Error("bad base32")
    bits += v.toString(2).padStart(5, "0")
  }
  const out: number[] = []
  for (let i = 0; i + 8 <= bits.length; i += 8) out.push(parseInt(bits.slice(i, i + 8), 2))
  return Buffer.from(out)
}

export function totp(secret: string, now = Date.now()): string {
  const step = Math.floor(now / 1000 / 30)
  const msg = Buffer.alloc(8)
  msg.writeBigUInt64BE(BigInt(step))
  const h = createHmac("sha1", base32(secret)).update(msg).digest()
  const off = h[h.length - 1] & 0x0f
  const v = h.readUInt32BE(off) & 0x7fffffff
  return String(v % 1_000_000).padStart(6, "0")
}
