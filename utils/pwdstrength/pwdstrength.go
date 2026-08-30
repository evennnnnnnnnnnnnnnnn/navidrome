// Package pwdstrength scores passwords as weak, medium or strong.
//
// The algorithm is deliberately simple and fully deterministic so that it can be
// mirrored exactly in the web UI (ui/src/utils/passwordStrength.js). Both
// implementations are pinned to the same table of test vectors
// (ui/src/utils/passwordStrengthVectors.json), so a change to one that is not
// mirrored in the other fails the build on both sides.
//
// Scoring is length-dominant, in line with NIST SP 800-63B: a long passphrase
// beats a short password with punctuation sprinkled on it. Composition is a
// secondary contributor rather than a hard gate, and a blocklist catches the
// passwords that pass every arithmetic rule but are the first thing an attacker
// tries.
package pwdstrength

import (
	"fmt"
	"strings"
	"unicode"
)

// Level is the strength bucket a password falls into.
type Level int

const (
	Weak Level = iota
	Medium
	Strong
)

func (l Level) String() string {
	switch l {
	case Strong:
		return "strong"
	case Medium:
		return "medium"
	default:
		return "weak"
	}
}

const (
	// MinLength is the floor below which a password is weak no matter what it contains.
	MinLength = 8
	// StrongLength is the length at which a password with 3+ character classes becomes strong.
	StrongLength = 12
	// PassphraseLength is the length at which 2 character classes is enough to be strong.
	PassphraseLength = 16
	// MaxLength bounds the work done on attacker-supplied input.
	MaxLength = 256
)

// Reason keys explain *why* a password scored the way it did. They are stable
// identifiers, not prose: the UI maps them to translated strings under
// resources.user.validation.passwordReason.*.
const (
	ReasonTooShort     = "tooShort"
	ReasonTooLong      = "tooLong"
	ReasonNeedsLength  = "needsLength"
	ReasonNeedsVariety = "needsVariety"
	ReasonCommon       = "commonPassword"
	ReasonHasUsername  = "containsUsername"
	ReasonHasEmail     = "containsEmail"
	ReasonRepeated     = "repeatedCharacters"
	ReasonSequential   = "sequentialCharacters"
)

// TooWeakI18nKey is the translation key returned to clients when a password
// fails the bar. The web UI resolves it; other callers can show it verbatim.
const TooWeakI18nKey = "resources.user.validation.passwordTooWeak"

// Result is the outcome of evaluating a password.
type Result struct {
	Level   Level    `json:"level"`
	Reasons []string `json:"reasons"`
}

// Evaluate scores password. username and email are optional context: a password
// built out of the account it protects is not a password. Either may be empty.
func Evaluate(password, username, email string) Result {
	runes := []rune(password)
	n := len(runes)

	if n == 0 {
		return Result{Level: Weak, Reasons: []string{ReasonTooShort}}
	}
	if n > MaxLength {
		return Result{Level: Weak, Reasons: []string{ReasonTooLong}}
	}

	var reasons []string
	add := func(r string) { reasons = append(reasons, r) }

	// Disqualifiers. Any one of these caps the password at weak, however long or
	// varied it is: they all describe a password an attacker guesses early.
	disqualified := false
	if isCommon(password) {
		add(ReasonCommon)
		disqualified = true
	}
	if containsCredential(password, username) {
		add(ReasonHasUsername)
		disqualified = true
	}
	if containsCredential(password, emailLocalPart(email)) {
		add(ReasonHasEmail)
		disqualified = true
	}
	if isRepetitive(runes) {
		add(ReasonRepeated)
		disqualified = true
	}
	if isSequential(password) {
		add(ReasonSequential)
		disqualified = true
	}

	classes := countClasses(runes)

	if n < MinLength {
		add(ReasonTooShort)
		return Result{Level: Weak, Reasons: reasons}
	}
	if disqualified {
		return Result{Level: Weak, Reasons: reasons}
	}

	// Strong is either "long enough with real variety" or "long enough that
	// variety stops mattering" — the passphrase case.
	if (n >= StrongLength && classes >= 3) || (n >= PassphraseLength && classes >= 2) {
		return Result{Level: Strong, Reasons: nil}
	}

	// Medium: say what would actually close the gap, rather than just refusing.
	var needsLength, needsVariety bool
	switch {
	case classes >= 3:
		// Variety is already there, so the only shortfall is length.
		needsLength = true
	case classes == 2:
		// Either grow to PassphraseLength, or add a class and reach StrongLength.
		// Only the second is available once it is already past StrongLength.
		needsLength = true
		needsVariety = n >= StrongLength
	default:
		// A single character class is never strong at any length.
		needsVariety = true
		needsLength = n < StrongLength
	}
	if needsLength {
		add(ReasonNeedsLength)
	}
	if needsVariety {
		add(ReasonNeedsVariety)
	}
	return Result{Level: Medium, Reasons: reasons}
}

// Validate returns nil when password is Strong, and an error naming the first
// reason it is not otherwise.
func Validate(password, username, email string) error {
	res := Evaluate(password, username, email)
	if res.Level == Strong {
		return nil
	}
	return fmt.Errorf("password is %s: %s", res.Level, strings.Join(res.Reasons, ", "))
}

// explanations render reason keys as English prose for the CLI. The web UI does
// not use these — it translates the same keys through i18n instead.
var explanations = map[string]string{
	ReasonTooShort:     fmt.Sprintf("use at least %d characters", MinLength),
	ReasonTooLong:      fmt.Sprintf("use at most %d characters", MaxLength),
	ReasonNeedsLength:  fmt.Sprintf("make it longer (%d+ characters, or %d+ for a passphrase)", StrongLength, PassphraseLength),
	ReasonNeedsVariety: "mix upper case, lower case, digits and symbols",
	ReasonCommon:       "it is based on a commonly guessed password",
	ReasonHasUsername:  "it contains the username",
	ReasonHasEmail:     "it contains the email address",
	ReasonRepeated:     "it just repeats the same few characters",
	ReasonSequential:   "it is a straight run of letters, digits or keyboard keys",
}

// Describe renders reason keys as a human-readable sentence fragment.
func Describe(reasons []string) string {
	parts := make([]string, 0, len(reasons))
	for _, r := range reasons {
		if text, ok := explanations[r]; ok {
			parts = append(parts, text)
		} else {
			parts = append(parts, r)
		}
	}
	return strings.Join(parts, "; ")
}

func countClasses(runes []rune) int {
	var lower, upper, digit, other bool
	for _, r := range runes {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	classes := 0
	for _, present := range []bool{lower, upper, digit, other} {
		if present {
			classes++
		}
	}
	return classes
}

// containsCredential reports whether password embeds an account identifier.
// Fragments shorter than 3 characters are ignored — they match too much.
func containsCredential(password, credential string) bool {
	if len([]rune(credential)) < 3 {
		return false
	}
	return strings.Contains(strings.ToLower(password), strings.ToLower(credential))
}

func emailLocalPart(email string) string {
	if at := strings.Index(email, "@"); at >= 0 {
		return email[:at]
	}
	return email
}

// isRepetitive catches "aaaaaaaaaaaa" and "abababababab": anything built by
// repeating a unit of 1-3 characters.
func isRepetitive(runes []rune) bool {
	n := len(runes)
	if n < 4 {
		return false
	}
	for unit := 1; unit <= 3; unit++ {
		if n%unit != 0 || n/unit < 2 {
			continue
		}
		matches := true
		for i := unit; i < n && matches; i++ {
			if unicode.ToLower(runes[i]) != unicode.ToLower(runes[i-unit]) {
				matches = false
			}
		}
		if matches {
			return true
		}
	}
	return false
}

// sequences are the runs people reach for when asked to invent a password.
var sequences = []string{
	"abcdefghijklmnopqrstuvwxyz",
	"0123456789",
	"qwertyuiop",
	"asdfghjkl",
	"zxcvbnm",
}

// isSequential reports whether the whole password is a slice of a known run,
// read forwards or backwards.
func isSequential(password string) bool {
	s := strings.ToLower(password)
	if len([]rune(s)) < 4 {
		return false
	}
	reversed := reverse(s)
	for _, seq := range sequences {
		if strings.Contains(seq, s) || strings.Contains(seq, reversed) {
			return true
		}
	}
	return false
}

func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// leetMap undoes the character substitutions that make a dictionary word look
// like it is not one.
var leetMap = map[rune]rune{
	'@': 'a', '4': 'a',
	'0': 'o',
	'1': 'i', '!': 'i',
	'3': 'e',
	'$': 's', '5': 's',
	'7': 't',
	'+': 't',
}

// normalizeForBlocklist reduces a password to the dictionary word it is dressed
// up as. "P@ssw0rd123!" and "passw0rd" both reduce to "password".
//
// Order matters: decorations are trimmed from the ends *before* leet
// substitution, so the trailing "123" in "Password123" is stripped as digits
// rather than being read as the letters "ie".
func normalizeForBlocklist(password string) string {
	s := strings.ToLower(password)
	s = strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	s = strings.TrimFunc(s, unicode.IsDigit)

	var b strings.Builder
	for _, r := range s {
		if sub, ok := leetMap[r]; ok {
			b.WriteRune(sub)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isCommon(password string) bool {
	if _, found := commonPasswords[strings.ToLower(password)]; found {
		return true
	}
	normalized := normalizeForBlocklist(password)
	if normalized == "" {
		return false
	}
	_, found := commonPasswords[normalized]
	return found
}

// NormalizeForBlocklist is exported for the test that asserts every blocklist entry
// is already in normalized form.
func NormalizeForBlocklist(password string) string {
	return normalizeForBlocklist(password)
}
