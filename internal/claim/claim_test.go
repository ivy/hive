package claim_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/claim"
)

var _ = Describe("TryClaim", func() {
	var dataDir string

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "claim-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(dataDir)
	})

	It("succeeds on first claim for a ref", func() {
		ok, err := claim.TryClaim(dataDir, "issue/42", "session-aaa")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
	})

	It("returns false on duplicate claim for same ref", func() {
		_, err := claim.TryClaim(dataDir, "issue/42", "session-aaa")
		Expect(err).NotTo(HaveOccurred())

		ok, err := claim.TryClaim(dataDir, "issue/42", "session-bbb")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
	})

	It("creates the claims directory if missing", func() {
		claimsDir := filepath.Join(dataDir, "claims")
		Expect(claimsDir).NotTo(BeADirectory())

		ok, err := claim.TryClaim(dataDir, "issue/1", "session-aaa")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(claimsDir).To(BeADirectory())
	})

	It("writes the session ID as file content", func() {
		_, err := claim.TryClaim(dataDir, "issue/42", "session-xyz")
		Expect(err).NotTo(HaveOccurred())

		sid, err := claim.SessionForRef(dataDir, "issue/42")
		Expect(err).NotTo(HaveOccurred())
		Expect(sid).To(Equal("session-xyz"))
	})

	It("produces deterministic keys — same ref always collides", func() {
		ok1, err := claim.TryClaim(dataDir, "issue/42", "s1")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok1).To(BeTrue())

		ok2, err := claim.TryClaim(dataDir, "issue/42", "s2")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok2).To(BeFalse())
	})

	It("does not collide on different refs", func() {
		ok1, err := claim.TryClaim(dataDir, "issue/42", "s1")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok1).To(BeTrue())

		ok2, err := claim.TryClaim(dataDir, "issue/99", "s2")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok2).To(BeTrue())
	})
})

var _ = Describe("Release", func() {
	var dataDir string

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "claim-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(dataDir)
	})

	It("removes the claim file", func() {
		_, err := claim.TryClaim(dataDir, "issue/42", "session-aaa")
		Expect(err).NotTo(HaveOccurred())
		Expect(claim.Exists(dataDir, "issue/42")).To(BeTrue())

		err = claim.Release(dataDir, "issue/42")
		Expect(err).NotTo(HaveOccurred())
		Expect(claim.Exists(dataDir, "issue/42")).To(BeFalse())
	})

	It("is idempotent for nonexistent claims", func() {
		err := claim.Release(dataDir, "issue/999")
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("Exists", func() {
	var dataDir string

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "claim-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(dataDir)
	})

	It("returns true for an active claim", func() {
		_, err := claim.TryClaim(dataDir, "issue/42", "session-aaa")
		Expect(err).NotTo(HaveOccurred())
		Expect(claim.Exists(dataDir, "issue/42")).To(BeTrue())
	})

	It("returns false for an unclaimed ref", func() {
		Expect(claim.Exists(dataDir, "issue/42")).To(BeFalse())
	})
})

var _ = Describe("SessionForRef", func() {
	var dataDir string

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "claim-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(dataDir)
	})

	It("reads the session ID from the claim file", func() {
		_, err := claim.TryClaim(dataDir, "issue/42", "session-abc-123")
		Expect(err).NotTo(HaveOccurred())

		sid, err := claim.SessionForRef(dataDir, "issue/42")
		Expect(err).NotTo(HaveOccurred())
		Expect(sid).To(Equal("session-abc-123"))
	})

	It("returns an error for an unclaimed ref", func() {
		_, err := claim.SessionForRef(dataDir, "issue/999")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ListAll", func() {
	var dataDir string

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "claim-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(dataDir)
	})

	It("returns all active claims", func() {
		_, err := claim.TryClaim(dataDir, "issue/1", "s1")
		Expect(err).NotTo(HaveOccurred())
		_, err = claim.TryClaim(dataDir, "issue/2", "s2")
		Expect(err).NotTo(HaveOccurred())

		claims, err := claim.ListAll(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims).To(HaveLen(2))

		sessionIDs := []string{claims[0].SessionID, claims[1].SessionID}
		Expect(sessionIDs).To(ConsistOf("s1", "s2"))
	})

	It("returns empty slice when claims dir does not exist", func() {
		claims, err := claim.ListAll(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims).To(BeEmpty())
	})
})
