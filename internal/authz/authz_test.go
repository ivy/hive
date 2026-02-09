package authz_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/authz"
)

var _ = Describe("IsAllowed", func() {
	It("returns true when the user is in the allowed list", func() {
		Expect(authz.IsAllowed("ivy", []string{"ivy", "oak"})).To(BeTrue())
	})

	It("returns false when the user is not in the allowed list", func() {
		Expect(authz.IsAllowed("stranger", []string{"ivy", "oak"})).To(BeFalse())
	})

	It("returns false for an empty allowed list", func() {
		Expect(authz.IsAllowed("ivy", []string{})).To(BeFalse())
	})

	It("returns false for a nil allowed list", func() {
		Expect(authz.IsAllowed("ivy", nil)).To(BeFalse())
	})

	It("compares case-insensitively", func() {
		Expect(authz.IsAllowed("IVY", []string{"ivy"})).To(BeTrue())
		Expect(authz.IsAllowed("ivy", []string{"IVY"})).To(BeTrue())
		Expect(authz.IsAllowed("Ivy", []string{"ivy"})).To(BeTrue())
	})
})
