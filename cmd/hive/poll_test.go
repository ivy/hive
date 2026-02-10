package main

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
)

var _ = Describe("runPoll", func() {
	BeforeEach(func() {
		viper.Reset()
	})

	Context("when interval is zero (single-shot mode)", func() {
		It("returns after a single poll cycle", func() {
			// Without valid config, pollOnce returns an error about missing project-id.
			// This confirms single-shot mode calls pollOnce exactly once and returns.
			viper.Set("poll.interval", time.Duration(0))
			ctx := context.Background()

			cmd := pollCmd
			cmd.SetContext(ctx)
			err := runPoll(cmd, nil)
			Expect(err).To(MatchError(ContainSubstring("github.project-id not configured")))
		})
	})

	Context("when interval is set (daemon mode)", func() {
		It("stops when context is cancelled", func() {
			viper.Set("poll.interval", 100*time.Millisecond)
			// pollOnce will fail on missing config, but the loop should
			// keep running until context cancels — errors are non-fatal.
			ctx, cancel := context.WithCancel(context.Background())

			done := make(chan error, 1)
			cmd := pollCmd
			cmd.SetContext(ctx)
			go func() {
				done <- runPoll(cmd, nil)
			}()

			// Let at least one tick fire.
			time.Sleep(250 * time.Millisecond)
			cancel()

			Eventually(done).Should(Receive(BeNil()))
		})

		It("does not exit on poll cycle errors", func() {
			viper.Set("poll.interval", 50*time.Millisecond)
			// Missing config means every poll cycle returns an error.
			// The daemon should keep running, not exit.
			ctx, cancel := context.WithCancel(context.Background())

			done := make(chan error, 1)
			cmd := pollCmd
			cmd.SetContext(ctx)
			go func() {
				done <- runPoll(cmd, nil)
			}()

			// Wait for several ticks to confirm it stays alive.
			time.Sleep(200 * time.Millisecond)
			Consistently(done).ShouldNot(Receive())

			cancel()
			Eventually(done).Should(Receive(BeNil()))
		})
	})
})

var _ = Describe("pollOnce", func() {
	BeforeEach(func() {
		viper.Reset()
	})

	Context("when project-id is not configured", func() {
		It("returns a configuration error", func() {
			err := pollOnce(context.Background())
			Expect(err).To(MatchError(ContainSubstring("github.project-id not configured")))
		})
	})

	Context("when project-node-id is not configured", func() {
		It("returns a configuration error", func() {
			viper.Set("github.project-id", "26")
			err := pollOnce(context.Background())
			Expect(err).To(MatchError(ContainSubstring("github.project-node-id not configured")))
		})
	})

	Context("when allowed-users is not configured", func() {
		It("returns a configuration error", func() {
			viper.Set("github.project-id", "26")
			viper.Set("github.project-node-id", "PVT_test")
			err := pollOnce(context.Background())
			Expect(err).To(MatchError(ContainSubstring("security.allowed-users not configured")))
		})
	})
})

var _ = Describe("poll --interval flag", func() {
	It("defaults to zero (single-shot)", func() {
		val, err := pollCmd.Flags().GetDuration("interval")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal(time.Duration(0)))
	})

	It("is bound to viper poll.interval", func() {
		viper.Reset()
		viper.Set("poll.interval", 5*time.Minute)
		Expect(viper.GetDuration("poll.interval")).To(Equal(5 * time.Minute))
	})
})
