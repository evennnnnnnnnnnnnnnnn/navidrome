// Password strength scoring, mirroring utils/pwdstrength/pwdstrength.go exactly.
//
// The Go side is authoritative — it is what actually refuses a weak password. This
// exists so the form can show a live meter and block submission before a round
// trip, and so the two never disagree about what "strong" means. Both are pinned
// to ./passwordStrengthVectors.json; change one without the other and the test
// suites fail on both sides.
//
// Unicode note: the class regexes use \p{Ll}/\p{Lu}/\p{Nd} with the `u` flag
// rather than [a-z]/[A-Z]/\d, because Go's unicode.IsLower/IsUpper/IsDigit are the
// Ll/Lu/Nd categories. A plain \d would disagree with Go on fullwidth digits.
import commonWords from './passwordCommonWords.json'

export const WEAK = 'weak'
export const MEDIUM = 'medium'
export const STRONG = 'strong'

export const MIN_LENGTH = 8
export const STRONG_LENGTH = 12
export const PASSPHRASE_LENGTH = 16
export const MAX_LENGTH = 256

export const REASON_TOO_SHORT = 'tooShort'
export const REASON_TOO_LONG = 'tooLong'
export const REASON_NEEDS_LENGTH = 'needsLength'
export const REASON_NEEDS_VARIETY = 'needsVariety'
export const REASON_COMMON = 'commonPassword'
export const REASON_HAS_USERNAME = 'containsUsername'
export const REASON_HAS_EMAIL = 'containsEmail'
export const REASON_REPEATED = 'repeatedCharacters'
export const REASON_SEQUENTIAL = 'sequentialCharacters'

const COMMON = new Set(commonWords.words)

const LOWER = /\p{Ll}/u
const UPPER = /\p{Lu}/u
const DIGIT = /\p{Nd}/u
const ALNUM = /[\p{L}\p{Nd}]/u

const SEQUENCES = [
  'abcdefghijklmnopqrstuvwxyz',
  '0123456789',
  'qwertyuiop',
  'asdfghjkl',
  'zxcvbnm',
]

const LEET = {
  '@': 'a',
  4: 'a',
  0: 'o',
  1: 'i',
  '!': 'i',
  3: 'e',
  $: 's',
  5: 's',
  7: 't',
  '+': 't',
}

const countClasses = (runes) => {
  let lower = false
  let upper = false
  let digit = false
  let other = false
  for (const r of runes) {
    if (LOWER.test(r)) lower = true
    else if (UPPER.test(r)) upper = true
    else if (DIGIT.test(r)) digit = true
    else other = true
  }
  return [lower, upper, digit, other].filter(Boolean).length
}

// Fragments shorter than 3 characters are ignored — they match too much.
const containsCredential = (password, credential) => {
  if (!credential || [...credential].length < 3) return false
  return password.toLowerCase().includes(credential.toLowerCase())
}

const emailLocalPart = (email) => {
  if (!email) return ''
  const at = email.indexOf('@')
  return at >= 0 ? email.slice(0, at) : email
}

// Catches "aaaaaaaaaaaa" and "abababababab": anything built by repeating a unit
// of 1-3 characters.
const isRepetitive = (runes) => {
  const n = runes.length
  if (n < 4) return false
  for (let unit = 1; unit <= 3; unit++) {
    if (n % unit !== 0 || n / unit < 2) continue
    let matches = true
    for (let i = unit; i < n && matches; i++) {
      if (runes[i].toLowerCase() !== runes[i - unit].toLowerCase())
        matches = false
    }
    if (matches) return true
  }
  return false
}

// True when the whole password is a slice of a known run, forwards or backwards.
const isSequential = (password) => {
  const s = password.toLowerCase()
  if ([...s].length < 4) return false
  const reversed = [...s].reverse().join('')
  return SEQUENCES.some((seq) => seq.includes(s) || seq.includes(reversed))
}

// Reduces a password to the dictionary word it is dressed up as: "P@ssw0rd123!"
// and "passw0rd" both reduce to "password".
//
// Order matters. Decorations come off the ends *before* leet substitution, so the
// trailing "123" in "Password123" is stripped as digits rather than read as "ie".
const normalizeForBlocklist = (password) => {
  let runes = [...password.toLowerCase()]
  const trim = (predicate) => {
    let start = 0
    let end = runes.length
    while (start < end && predicate(runes[start])) start++
    while (end > start && predicate(runes[end - 1])) end--
    runes = runes.slice(start, end)
  }
  trim((r) => !ALNUM.test(r))
  trim((r) => DIGIT.test(r))
  return runes.map((r) => LEET[r] ?? r).join('')
}

const isCommon = (password) => {
  if (COMMON.has(password.toLowerCase())) return true
  const normalized = normalizeForBlocklist(password)
  return normalized !== '' && COMMON.has(normalized)
}

/**
 * Scores a password.
 *
 * @param {string} password
 * @param {string} [username] the account's username, if known
 * @param {string} [email] the account's email, if known
 * @returns {{level: 'weak'|'medium'|'strong', reasons: string[]}}
 */
export const evaluatePassword = (password, username = '', email = '') => {
  const runes = [...(password ?? '')]
  const n = runes.length

  if (n === 0) return { level: WEAK, reasons: [REASON_TOO_SHORT] }
  if (n > MAX_LENGTH) return { level: WEAK, reasons: [REASON_TOO_LONG] }

  const reasons = []

  // Disqualifiers. Any one caps the password at weak, however long or varied: they
  // all describe a password an attacker guesses early.
  let disqualified = false
  if (isCommon(password)) {
    reasons.push(REASON_COMMON)
    disqualified = true
  }
  if (containsCredential(password, username)) {
    reasons.push(REASON_HAS_USERNAME)
    disqualified = true
  }
  if (containsCredential(password, emailLocalPart(email))) {
    reasons.push(REASON_HAS_EMAIL)
    disqualified = true
  }
  if (isRepetitive(runes)) {
    reasons.push(REASON_REPEATED)
    disqualified = true
  }
  if (isSequential(password)) {
    reasons.push(REASON_SEQUENTIAL)
    disqualified = true
  }

  const classes = countClasses(runes)

  if (n < MIN_LENGTH) {
    reasons.push(REASON_TOO_SHORT)
    return { level: WEAK, reasons }
  }
  if (disqualified) return { level: WEAK, reasons }

  if (
    (n >= STRONG_LENGTH && classes >= 3) ||
    (n >= PASSPHRASE_LENGTH && classes >= 2)
  ) {
    return { level: STRONG, reasons: [] }
  }

  // Say what would actually close the gap, rather than just refusing.
  let needsLength
  let needsVariety
  if (classes >= 3) {
    // Variety is already there, so the only shortfall is length.
    needsLength = true
    needsVariety = false
  } else if (classes === 2) {
    // Either grow to PASSPHRASE_LENGTH, or add a class and reach STRONG_LENGTH.
    needsLength = true
    needsVariety = n >= STRONG_LENGTH
  } else {
    // A single character class is never strong at any length.
    needsVariety = true
    needsLength = n < STRONG_LENGTH
  }
  if (needsLength) reasons.push(REASON_NEEDS_LENGTH)
  if (needsVariety) reasons.push(REASON_NEEDS_VARIETY)
  return { level: MEDIUM, reasons }
}

export const isStrongPassword = (password, username, email) =>
  evaluatePassword(password, username, email).level === STRONG
