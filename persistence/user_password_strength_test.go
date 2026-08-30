package persistence

import (
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These cover the two paths a person actually sets a password through the web UI:
// POST /api/user (Save) and PUT /api/user/{id} (Update). The first-run wizard and
// the CLI are covered in server/auth_test.go and cmd respectively, and the scoring
// itself in utils/pwdstrength.
var _ = Describe("UserRepository password strength", func() {
	var repo rest.Persistable
	var users model.UserRepository

	expectRejected := func(err error) {
		GinkgoHelper()
		Expect(err).To(HaveOccurred())
		vErr, ok := err.(*rest.ValidationError)
		Expect(ok).To(BeTrue(), "expected a rest.ValidationError, got %T", err)
		Expect(vErr.Errors).
			To(HaveKeyWithValue("password", "resources.user.validation.passwordTooWeak"))
	}

	BeforeEach(func() {
		// Update() decrypts the acting user's stored password before validating, so
		// the context user has to be a real, persisted user carrying the *encrypted*
		// password — which is exactly what FindByUsername returns, and what the auth
		// middleware puts in the context in production (server/auth.go:259).
		bootstrap := NewUserRepository(log.NewContext(GinkgoT().Context()), GetDBXBuilder())
		admin := model.User{
			ID: "pwd-admin-id", UserName: "pwdadmin", Name: "Pwd Admin",
			IsAdmin: true, NewPassword: "Admin-Horse-Battery-9",
		}
		Expect(bootstrap.Put(&admin)).To(Succeed())
		stored, err := bootstrap.FindByUsername("pwdadmin")
		Expect(err).ToNot(HaveOccurred())

		adminCtx := request.WithUser(log.NewContext(GinkgoT().Context()), *stored)
		adminCtx = request.WithTokenEpochHolder(adminCtx)
		ur := NewUserRepository(adminCtx, GetDBXBuilder())
		repo = ur.(rest.Persistable)
		users = ur.(model.UserRepository)
	})

	Describe("Save (create user)", func() {
		newUser := func(password string) *model.User {
			return &model.User{
				ID: id.NewRandom(), UserName: "pwd-" + id.NewRandom(),
				Name: "Created", NewPassword: password,
			}
		}

		DescribeTable("refuses a password that is not strong",
			func(password string) {
				u := newUser(password)
				_, err := repo.Save(u)
				expectRejected(err)

				// And no account is left behind.
				_, err = users.FindByUsername(u.UserName)
				Expect(err).To(MatchError(model.ErrNotFound))
			},
			Entry("too short", "secret"),
			Entry("common word dressed up", "Password123!"),
			Entry("leetspeak does not help", "P@ssw0rd!"),
			Entry("keyboard run", "qwertyui"),
			Entry("repeated unit", "abababababab"),
			Entry("medium is not enough", "Tr0ub4dor&3"),
			Entry("long but single-class", "correcthorsebatterystaple"),
		)

		DescribeTable("accepts a strong password",
			func(password string) {
				u := newUser(password)
				_, err := repo.Save(u)
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() { _ = users.Delete(u.ID) })

				saved, err := users.FindByUsernameWithPassword(u.UserName)
				Expect(err).ToNot(HaveOccurred())
				Expect(saved.Password).To(Equal(password))
			},
			Entry("mixed classes", "Correct-Horse-Battery-9"),
			Entry("passphrase carried by length", "the quick brown fox jumps"),
			Entry("non-ASCII", "日本語のパスワードです2026年"),
		)

		It("refuses a password built from the new account's own username", func() {
			u := &model.User{
				ID: id.NewRandom(), UserName: "hoshimachi", Name: "Suisei",
				NewPassword: "hoshimachi-Battery-9",
			}
			_, err := repo.Save(u)
			expectRejected(err)
		})

		It("allows creating a user with no password at all", func() {
			// An empty NewPassword means "leave the password alone", not "set it to
			// empty", so the strength bar must not fire on it.
			u := &model.User{ID: id.NewRandom(), UserName: "nopwd-" + id.NewRandom(), Name: "No Password"}
			_, err := repo.Save(u)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { _ = users.Delete(u.ID) })
		})
	})

	Describe("Update (change password)", func() {
		var target model.User

		BeforeEach(func() {
			target = model.User{
				ID: id.NewRandom(), UserName: "changer-" + id.NewRandom(),
				Name: "Changer", NewPassword: "Initial-Horse-Battery-9",
			}
			// Seeded through Put, which is deliberately not gated — only the entry
			// points are, so existing fixtures keep working.
			Expect(users.Put(&target)).To(Succeed())
			DeferCleanup(func() { _ = users.Delete(target.ID) })
		})

		It("refuses a weak new password and leaves the stored one intact", func() {
			upd := target
			upd.NewPassword = "secret"
			expectRejected(repo.Update(target.ID, &upd))

			reloaded, err := users.FindByUsernameWithPassword(target.UserName)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Password).To(Equal("Initial-Horse-Battery-9"))
		})

		It("accepts a strong new password", func() {
			upd := target
			upd.NewPassword = "Rotated-Horse-Battery-9"
			Expect(repo.Update(target.ID, &upd)).To(Succeed())

			reloaded, err := users.FindByUsernameWithPassword(target.UserName)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Password).To(Equal("Rotated-Horse-Battery-9"))
		})

		It("leaves an update that does not touch the password alone", func() {
			upd := target
			upd.NewPassword = ""
			upd.Name = "Renamed"
			Expect(repo.Update(target.ID, &upd)).To(Succeed())

			reloaded, err := users.Get(target.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(reloaded.Name).To(Equal("Renamed"))
		})

		It("holds an admin changing someone else's password to the same bar", func() {
			// validatePasswordChange returns early for this case, waiving the
			// current-password requirement entirely. The strength check must still run.
			upd := target
			upd.NewPassword = "Password123!"
			expectRejected(repo.Update(target.ID, &upd))
		})
	})
})
