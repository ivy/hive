package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"

	"github.com/ivy/hive/internal/claim"
	"github.com/ivy/hive/internal/session"
)

var _ = Describe("reap command", func() {
	It("is registered on the root command", func() {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Use == "reap" {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue())
	})

	It("accepts no arguments", func() {
		Expect(reapCmd.Args).NotTo(BeNil())
	})
})

var _ = Describe("reap config defaults", func() {
	BeforeEach(func() {
		viper.Reset()
	})

	It("defaults published-retention to 24h", func() {
		val, err := reapCmd.Flags().GetDuration("published-retention")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal(24 * time.Hour))
	})

	It("defaults failed-retention to 72h", func() {
		val, err := reapCmd.Flags().GetDuration("failed-retention")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal(72 * time.Hour))
	})

	It("binds published-retention to viper", func() {
		viper.Set("reap.published-retention", 48*time.Hour)
		Expect(viper.GetDuration("reap.published-retention")).To(Equal(48 * time.Hour))
	})

	It("binds failed-retention to viper", func() {
		viper.Set("reap.failed-retention", 168*time.Hour)
		Expect(viper.GetDuration("reap.failed-retention")).To(Equal(168 * time.Hour))
	})
})

var _ = Describe("reapSessions", func() {
	var (
		ctx     context.Context
		dataDir string
		wsDir   string
		deps    reapDeps

		// Track calls to injected deps.
		releasedRefs   []string
		removedWSIDs   []string
		unitActiveMap  map[string]bool
	)

	BeforeEach(func() {
		ctx = context.Background()

		tmpDir, err := os.MkdirTemp("", "hive-reap-test-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { os.RemoveAll(tmpDir) })

		dataDir = tmpDir
		wsDir = filepath.Join(tmpDir, "workspaces")
		Expect(os.MkdirAll(wsDir, 0o755)).To(Succeed())

		releasedRefs = nil
		removedWSIDs = nil
		unitActiveMap = make(map[string]bool)

		deps = reapDeps{
			isUnitActive: func(_ context.Context, id string) bool {
				return unitActiveMap[id]
			},
			releaseOnSource: func(_ context.Context, sess *session.Session) {
				releasedRefs = append(releasedRefs, sess.Ref)
			},
			removeWorkspace: func(_ context.Context, sessID string) {
				removedWSIDs = append(removedWSIDs, sessID)
				os.RemoveAll(filepath.Join(wsDir, sessID))
			},
		}
	})

	// Helper: create a session with the given fields and optionally a claim + workspace dir.
	createFixture := func(id, ref string, status session.Status, age time.Duration) {
		sess := &session.Session{
			ID:        id,
			Ref:       ref,
			Repo:      "ivy/hive",
			Status:    status,
			CreatedAt: time.Now().UTC().Add(-age),
		}
		Expect(session.Create(dataDir, sess)).To(Succeed())

		ok, err := claim.TryClaim(dataDir, ref, id)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		Expect(os.MkdirAll(filepath.Join(wsDir, id), 0o755)).To(Succeed())
	}

	sessionExists := func(id string) bool {
		_, err := session.Load(dataDir, id)
		return err == nil
	}

	Describe("expired published sessions", func() {
		It("reaps session, claim, and workspace when past retention", func() {
			createFixture("pub-old", "github:ivy/hive#10", session.StatusPublished, 48*time.Hour)

			err := reapSessions(ctx, dataDir, 24*time.Hour, 72*time.Hour, deps)
			Expect(err).NotTo(HaveOccurred())

			Expect(sessionExists("pub-old")).To(BeFalse())
			Expect(claim.Exists(dataDir, "github:ivy/hive#10")).To(BeFalse())
			Expect(filepath.Join(wsDir, "pub-old")).NotTo(BeADirectory())
			Expect(removedWSIDs).To(ContainElement("pub-old"))
		})
	})

	Describe("expired failed sessions", func() {
		It("reaps session, claim, and workspace when past retention", func() {
			createFixture("fail-old", "github:ivy/hive#20", session.StatusFailed, 96*time.Hour)

			err := reapSessions(ctx, dataDir, 24*time.Hour, 72*time.Hour, deps)
			Expect(err).NotTo(HaveOccurred())

			Expect(sessionExists("fail-old")).To(BeFalse())
			Expect(claim.Exists(dataDir, "github:ivy/hive#20")).To(BeFalse())
			Expect(filepath.Join(wsDir, "fail-old")).NotTo(BeADirectory())
			Expect(removedWSIDs).To(ContainElement("fail-old"))
		})
	})

	Describe("non-expired sessions", func() {
		It("leaves published sessions within retention untouched", func() {
			createFixture("pub-fresh", "github:ivy/hive#30", session.StatusPublished, 1*time.Hour)

			err := reapSessions(ctx, dataDir, 24*time.Hour, 72*time.Hour, deps)
			Expect(err).NotTo(HaveOccurred())

			Expect(sessionExists("pub-fresh")).To(BeTrue())
			Expect(claim.Exists(dataDir, "github:ivy/hive#30")).To(BeTrue())
			Expect(filepath.Join(wsDir, "pub-fresh")).To(BeADirectory())
			Expect(removedWSIDs).To(BeEmpty())
		})

		It("leaves failed sessions within retention untouched", func() {
			createFixture("fail-fresh", "github:ivy/hive#31", session.StatusFailed, 24*time.Hour)

			err := reapSessions(ctx, dataDir, 24*time.Hour, 72*time.Hour, deps)
			Expect(err).NotTo(HaveOccurred())

			Expect(sessionExists("fail-fresh")).To(BeTrue())
			Expect(claim.Exists(dataDir, "github:ivy/hive#31")).To(BeTrue())
			Expect(filepath.Join(wsDir, "fail-fresh")).To(BeADirectory())
			Expect(removedWSIDs).To(BeEmpty())
		})
	})

	Describe("stale non-terminal sessions", func() {
		It("marks as failed and releases claim when unit is inactive", func() {
			createFixture("run-stale", "github:ivy/hive#40", session.StatusRunning, 2*time.Hour)
			unitActiveMap["run-stale"] = false

			err := reapSessions(ctx, dataDir, 24*time.Hour, 72*time.Hour, deps)
			Expect(err).NotTo(HaveOccurred())

			loaded, err := session.Load(dataDir, "run-stale")
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.Status).To(Equal(session.StatusFailed))

			Expect(claim.Exists(dataDir, "github:ivy/hive#40")).To(BeFalse())
			Expect(releasedRefs).To(ContainElement("github:ivy/hive#40"))
		})
	})

	Describe("active non-terminal sessions", func() {
		It("skips sessions whose unit is still active", func() {
			createFixture("run-active", "github:ivy/hive#50", session.StatusRunning, 2*time.Hour)
			unitActiveMap["run-active"] = true

			err := reapSessions(ctx, dataDir, 24*time.Hour, 72*time.Hour, deps)
			Expect(err).NotTo(HaveOccurred())

			loaded, err := session.Load(dataDir, "run-active")
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.Status).To(Equal(session.StatusRunning))

			Expect(claim.Exists(dataDir, "github:ivy/hive#50")).To(BeTrue())
			Expect(filepath.Join(wsDir, "run-active")).To(BeADirectory())
			Expect(releasedRefs).To(BeEmpty())
		})
	})

	Describe("empty session list", func() {
		It("returns nil with no sessions", func() {
			err := reapSessions(ctx, dataDir, 24*time.Hour, 72*time.Hour, deps)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
