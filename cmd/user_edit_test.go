package cmd

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("user edit", func() {
	It("selects the prospective email for password validation", func() {
		Expect(prospectiveEmail("old@example.com", "new@example.com", false)).To(Equal("new@example.com"))
		Expect(prospectiveEmail("old@example.com", "", true)).To(BeEmpty())
		Expect(prospectiveEmail("old@example.com", "", false)).To(Equal("old@example.com"))
	})
})
