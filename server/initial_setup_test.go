package server

import (
	"context"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("initial_setup", func() {
	var ds model.DataStore

	BeforeEach(func() {
		ds = &tests.MockDataStore{}
	})

	Describe("createInitialAdminUser", func() {
		It("creates a new admin user with specified password if User table is empty", func() {
			Expect(createInitialAdminUser(ds, "Correct-Horse-Battery-9")).To(BeNil())
			ur := ds.User(context.TODO())
			admin, err := ur.FindByUsername("admin")
			Expect(err).To(BeNil())
			Expect(admin.Password).To(Equal("Correct-Horse-Battery-9"))
		})

		It("does not create a new admin user if User table is not empty", func() {
			Expect(createInitialAdminUser(ds, "First-Horse-Battery-9")).To(BeNil())
			ur := ds.User(context.TODO())
			Expect(ur.CountAll()).To(Equal(int64(1)))
			Expect(createInitialAdminUser(ds, "Second-Horse-Battery-9")).To(BeNil())
			Expect(ur.CountAll()).To(Equal(int64(1)))
		})
	})
})
