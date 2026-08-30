package nativeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeMaintenance records what the router asked for and returns a canned answer, so the
// tests below are about routing, gating and status codes rather than about disk work.
type fakeMaintenance struct {
	core.Maintenance
	songIDs   []string
	albumIDs  []string
	result    *core.DeletionResult
	returnErr error
}

func (f *fakeMaintenance) DeleteMediaFiles(_ context.Context, ids []string) (*core.DeletionResult, error) {
	f.songIDs = ids
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return f.result, nil
}

func (f *fakeMaintenance) DeleteAlbums(_ context.Context, ids []string) (*core.DeletionResult, error) {
	f.albumIDs = ids
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return f.result, nil
}

var _ = Describe("Deletion API", func() {
	var (
		ds          model.DataStore
		router      http.Handler
		maintenance *fakeMaintenance
		adminToken  string
		userToken   string
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		conf.Server.Deletion.Enabled = true

		ds = &tests.MockDataStore{}
		auth.Init(ds)

		maintenance = &fakeMaintenance{
			result: &core.DeletionResult{DeletedIDs: []string{"mf1"}, Count: 1, TrashFolder: "/data/trash/batch"},
		}
		nativeRouter := New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			maintenance, nil, nil, nil, nil)
		router = server.JWTVerifier(nativeRouter)

		adminUser := model.User{ID: "admin-1", UserName: "admin", Name: "Admin", IsAdmin: true, NewPassword: "adminpass"}
		regularUser := model.User{ID: "user-1", UserName: "regular", Name: "Regular", IsAdmin: false, NewPassword: "userpass"}
		Expect(ds.User(context.TODO()).Put(&adminUser)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&regularUser)).To(Succeed())

		var err error
		adminToken, err = auth.CreateToken(&adminUser)
		Expect(err).ToNot(HaveOccurred())
		userToken, err = auth.CreateToken(&regularUser)
		Expect(err).ToNot(HaveOccurred())
	})

	send := func(path, token string) *httptest.ResponseRecorder {
		req := createAuthenticatedRequest("DELETE", path, nil, token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	Describe("authorization", func() {
		It("rejects an unauthenticated request", func() {
			req := createUnauthenticatedRequest("DELETE", "/deletion/song?id=mf1", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusUnauthorized))
			Expect(maintenance.songIDs).To(BeNil(), "the service must never be reached")
		})

		It("rejects a non-admin user on every deletion route", func() {
			for _, path := range []string{"/deletion/song?id=mf1", "/deletion/album?id=al1"} {
				w := send(path, userToken)
				Expect(w.Code).To(Equal(http.StatusForbidden), "path %s", path)
			}
			Expect(maintenance.songIDs).To(BeNil())
			Expect(maintenance.albumIDs).To(BeNil())
		})

		It("allows an admin", func() {
			w := send("/deletion/song?id=mf1", adminToken)
			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("songs", func() {
		It("passes every id through and returns the result", func() {
			w := send("/deletion/song?id=mf1&id=mf2", adminToken)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(maintenance.songIDs).To(Equal([]string{"mf1", "mf2"}))

			var body core.DeletionResult
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body.DeletedIDs).To(ConsistOf("mf1"))
			Expect(body.Count).To(Equal(1))
			Expect(body.TrashFolder).To(Equal("/data/trash/batch"))
		})

		It("does not treat a missing id list as 'delete everything'", func() {
			maintenance.returnErr = core.ErrNoIDs
			w := send("/deletion/song", adminToken)
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(maintenance.songIDs).To(BeEmpty())
		})
	})

	Describe("albums", func() {
		It("passes album ids through", func() {
			w := send("/deletion/album?id=al1&id=al2", adminToken)
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(maintenance.albumIDs).To(Equal([]string{"al1", "al2"}))
			Expect(maintenance.songIDs).To(BeNil())
		})
	})

	DescribeTable("error mapping",
		func(err error, expected int) {
			maintenance.returnErr = err
			w := send("/deletion/song?id=mf1", adminToken)
			Expect(w.Code).To(Equal(expected))
		},
		Entry("disabled feature is forbidden", core.ErrDeletionDisabled, http.StatusForbidden),
		Entry("non-admin at the service layer is forbidden", core.ErrDeletionNotAdmin, http.StatusForbidden),
		Entry("empty ids is a bad request", core.ErrNoIDs, http.StatusBadRequest),
		Entry("unknown ids is not found", model.ErrNotFound, http.StatusNotFound),
		Entry("an unsafe path is a bad request", fmt.Errorf("%w: nope", core.ErrUnsafeDeletion), http.StatusBadRequest),
		Entry("anything else is a server error", fmt.Errorf("disk on fire"), http.StatusInternalServerError),
	)
})
