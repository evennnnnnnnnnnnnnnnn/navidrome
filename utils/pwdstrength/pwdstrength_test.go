package pwdstrength_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/navidrome/navidrome/utils/pwdstrength"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// sharedVectorsPath is the single source of truth shared with the web UI. The JS
// mirror (ui/src/utils/passwordStrength.js) is tested against this exact file, so
// the two implementations cannot drift apart without a test failing on both sides.
const sharedVectorsPath = "../../ui/src/utils/passwordStrengthVectors.json"

// sharedWordsPath holds the blocklist the UI actually loads at runtime. The Go copy
// in common.go is a literal for embedding reasons, so it is pinned to this file by
// a test rather than by construction — adding a word to one and not the other fails.
const sharedWordsPath = "../../ui/src/utils/passwordCommonWords.json"

type vector struct {
	Note     string   `json:"note"`
	Password string   `json:"password"`
	UserName string   `json:"username"`
	Email    string   `json:"email"`
	Level    string   `json:"level"`
	Reasons  []string `json:"reasons"`
}

var _ = Describe("pwdstrength", func() {
	Describe("shared vectors", func() {
		var vectors []vector

		BeforeEach(func() {
			data, err := os.ReadFile(sharedVectorsPath)
			Expect(err).ToNot(HaveOccurred(), "the UI mirror's vector file must exist")
			var doc struct {
				Vectors []vector `json:"vectors"`
			}
			Expect(json.Unmarshal(data, &doc)).To(Succeed())
			vectors = doc.Vectors
			Expect(vectors).ToNot(BeEmpty())
		})

		It("matches every shared vector", func() {
			for _, v := range vectors {
				res := pwdstrength.Evaluate(v.Password, v.UserName, v.Email)
				desc := fmt.Sprintf("vector %q (%s)", truncate(v.Password), v.Note)
				Expect(res.Level.String()).To(Equal(v.Level), desc)

				reasons := res.Reasons
				if reasons == nil {
					reasons = []string{}
				}
				want := v.Reasons
				if want == nil {
					want = []string{}
				}
				Expect(reasons).To(Equal(want), desc+" reasons")
			}
		})

		It("agrees with Validate on every shared vector", func() {
			for _, v := range vectors {
				err := pwdstrength.Validate(v.Password, v.UserName, v.Email)
				if v.Level == "strong" {
					Expect(err).ToNot(HaveOccurred(), truncate(v.Password))
				} else {
					Expect(err).To(HaveOccurred(), truncate(v.Password))
				}
			}
		})
	})

	Describe("the shared blocklist", func() {
		It("is identical to the one the UI loads", func() {
			data, err := os.ReadFile(sharedWordsPath)
			Expect(err).ToNot(HaveOccurred())
			var doc struct {
				Words []string `json:"words"`
			}
			Expect(json.Unmarshal(data, &doc)).To(Succeed())
			Expect(doc.Words).To(ConsistOf(pwdstrength.CommonWords()))
		})

		It("holds only already-normalized entries", func() {
			// An entry that does not survive normalization can never match, because
			// lookups happen on the normalized form. Catches "Password123" being added
			// where "password" was meant.
			for _, w := range pwdstrength.CommonWords() {
				Expect(pwdstrength.NormalizeForBlocklist(w)).To(Equal(w),
					"blocklist entry %q is not in normalized form", w)
			}
		})

		It("has no duplicates", func() {
			seen := map[string]bool{}
			for _, w := range pwdstrength.CommonWords() {
				Expect(seen[w]).To(BeFalse(), "duplicate blocklist entry %q", w)
				seen[w] = true
			}
		})
	})

	Describe("Evaluate", func() {
		It("treats the blocklist case-insensitively", func() {
			for _, pw := range []string{"password", "PASSWORD", "PaSsWoRd"} {
				Expect(pwdstrength.Evaluate(pw, "", "").Reasons).
					To(ContainElement(pwdstrength.ReasonCommon), pw)
			}
		})

		It("does not flag a long password merely for containing a common word", func() {
			// The blocklist matches the whole normalized password, not substrings —
			// otherwise every passphrase with an ordinary word in it would be rejected.
			res := pwdstrength.Evaluate("the-password-vault-42", "", "")
			Expect(res.Reasons).ToNot(ContainElement(pwdstrength.ReasonCommon))
			Expect(res.Level).To(Equal(pwdstrength.Strong))
		})

		It("ignores an empty username or email", func() {
			Expect(pwdstrength.Evaluate("Zq7-vnPtk2Lm", "", "").Level).To(Equal(pwdstrength.Strong))
		})

		It("matches the username regardless of case", func() {
			res := pwdstrength.Evaluate("XXEVENxx-Battery-9", "even", "")
			Expect(res.Reasons).To(ContainElement(pwdstrength.ReasonHasUsername))
			Expect(res.Level).To(Equal(pwdstrength.Weak))
		})

		It("accepts an email with no @ as a bare local part", func() {
			res := pwdstrength.Evaluate("yiwenz-Battery-9x", "", "yiwenz")
			Expect(res.Reasons).To(ContainElement(pwdstrength.ReasonHasEmail))
		})

		It("never reports strong with reasons attached", func() {
			res := pwdstrength.Evaluate("Correct-Horse-Battery-9", "", "")
			Expect(res.Level).To(Equal(pwdstrength.Strong))
			Expect(res.Reasons).To(BeEmpty())
		})

		It("accepts a password exactly at MaxLength and rejects one past it", func() {
			// Padding with a repeated rune would trip isRepetitive, so vary it.
			atMax := strings.Repeat("aB3$", pwdstrength.MaxLength/4)
			Expect(atMax).To(HaveLen(pwdstrength.MaxLength))
			Expect(pwdstrength.Evaluate(atMax, "", "").Reasons).
				ToNot(ContainElement(pwdstrength.ReasonTooLong))

			Expect(pwdstrength.Evaluate(atMax+"x", "", "").Reasons).
				To(Equal([]string{pwdstrength.ReasonTooLong}))
		})

		It("counts unicode letters and digits by category, matching the JS mirror", func() {
			// Fullwidth digits are unicode.Nd, which \p{Nd} also matches in JS.
			Expect(pwdstrength.Evaluate("日本語のパスワードです２０２６年", "", "").Level).
				To(Equal(pwdstrength.Strong))
		})
	})

	Describe("Validate", func() {
		It("returns nil for a strong password", func() {
			Expect(pwdstrength.Validate("Correct-Horse-Battery-9", "", "")).To(Succeed())
		})

		It("names the level and the reasons", func() {
			err := pwdstrength.Validate("password", "", "")
			Expect(err).To(MatchError(ContainSubstring("weak")))
			Expect(err).To(MatchError(ContainSubstring(pwdstrength.ReasonCommon)))
		})
	})
})

func truncate(s string) string {
	if len(s) <= 32 {
		return s
	}
	return s[:32] + fmt.Sprintf("...(%d)", len(s))
}
