import { describe, it, expect } from 'vitest'
import {
  evaluatePassword,
  isStrongPassword,
  STRONG,
  WEAK,
  MAX_LENGTH,
  REASON_COMMON,
  REASON_HAS_USERNAME,
  REASON_HAS_EMAIL,
  REASON_TOO_LONG,
} from './passwordStrength'
import vectorsDoc from './passwordStrengthVectors.json'

// This same file drives utils/pwdstrength/pwdstrength_test.go. If the Go scorer and
// this one ever disagree, one of the two suites goes red — which is the point.
describe('passwordStrength shared vectors', () => {
  const { vectors } = vectorsDoc

  it('has vectors to check', () => {
    expect(vectors.length).toBeGreaterThan(20)
  })

  it.each(vectors)(
    'scores $level: $note',
    ({ password, username, email, level, reasons }) => {
      const result = evaluatePassword(password, username, email)
      expect(result.level).toBe(level)
      expect(result.reasons).toEqual(reasons)
    },
  )

  it.each(vectors)(
    'isStrongPassword agrees for $level: $note',
    ({ password, username, email, level }) => {
      expect(isStrongPassword(password, username, email)).toBe(level === STRONG)
    },
  )
})

describe('evaluatePassword', () => {
  it('treats the blocklist case-insensitively', () => {
    for (const pw of ['password', 'PASSWORD', 'PaSsWoRd']) {
      expect(evaluatePassword(pw).reasons).toContain(REASON_COMMON)
    }
  })

  it('does not flag a long password merely for containing a common word', () => {
    // The blocklist matches the whole normalized password, not substrings —
    // otherwise every passphrase with an ordinary word in it would be rejected.
    const result = evaluatePassword('the-password-vault-42')
    expect(result.reasons).not.toContain(REASON_COMMON)
    expect(result.level).toBe(STRONG)
  })

  it('matches the username regardless of case', () => {
    const result = evaluatePassword('XXEVENxx-Battery-9', 'even')
    expect(result.reasons).toContain(REASON_HAS_USERNAME)
    expect(result.level).toBe(WEAK)
  })

  it('accepts an email with no @ as a bare local part', () => {
    expect(
      evaluatePassword('yiwenz-Battery-9x', '', 'yiwenz').reasons,
    ).toContain(REASON_HAS_EMAIL)
  })

  it('handles null and undefined without throwing', () => {
    expect(evaluatePassword(undefined).level).toBe(WEAK)
    expect(evaluatePassword(null).level).toBe(WEAK)
  })

  it('never reports strong with reasons attached', () => {
    const result = evaluatePassword('Correct-Horse-Battery-9')
    expect(result.level).toBe(STRONG)
    expect(result.reasons).toEqual([])
  })

  it('accepts a password exactly at MAX_LENGTH and rejects one past it', () => {
    // Padding with a repeated rune would trip the repetition rule, so vary it.
    const atMax = 'aB3$'.repeat(MAX_LENGTH / 4)
    expect(atMax).toHaveLength(MAX_LENGTH)
    expect(evaluatePassword(atMax).reasons).not.toContain(REASON_TOO_LONG)
    expect(evaluatePassword(atMax + 'x').reasons).toEqual([REASON_TOO_LONG])
  })

  it('counts unicode classes by category, matching Go', () => {
    // Fullwidth digits are Nd, which unicode.IsDigit also matches. A plain \d
    // would not, and this would score differently from the server.
    expect(evaluatePassword('日本語のパスワードです２０２６年').level).toBe(
      STRONG,
    )
  })
})
