package cmd

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("user edit", func() {
	It("validates a new password against the replacement email", func() {
		Expect(prospectiveEmail("old@example.com", "new@example.com", false)).To(Equal("new@example.com"))
	})
})
