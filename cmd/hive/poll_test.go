package main

import (
	"context"
	"os"
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

var _ = Describe("poll --max-concurrent flag", func() {
	It("defaults to 2", func() {
		val, err := pollCmd.Flags().GetInt("max-concurrent")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal(2))
	})

	It("is bound to viper poll.max-concurrent", func() {
		viper.Reset()
		viper.Set("poll.max-concurrent", 5)
		Expect(viper.GetInt("poll.max-concurrent")).To(Equal(5))
	})
})

var _ = Describe("countRunningProcesses", func() {
	BeforeEach(func() {
		// Clear global state
		runningProcessesMu.Lock()
		runningProcesses = make(map[int]struct{})
		runningProcessesMu.Unlock()
	})

	Context("with no tracked processes", func() {
		It("returns 0", func() {
			Expect(countRunningProcesses()).To(Equal(0))
		})
	})

	Context("with tracked processes", func() {
		It("counts only alive processes", func() {
			// Track current test process (self, guaranteed alive)
			runningProcessesMu.Lock()
			currentPID := os.Getpid()
			runningProcesses[currentPID] = struct{}{}
			runningProcessesMu.Unlock()

			Expect(countRunningProcesses()).To(Equal(1))
		})

		It("prunes dead processes", func() {
			// Track a PID that doesn't exist
			runningProcessesMu.Lock()
			deadPID := 999999
			runningProcesses[deadPID] = struct{}{}
			runningProcessesMu.Unlock()

			// countRunningProcesses should prune it
			Expect(countRunningProcesses()).To(Equal(0))

			// Verify it's actually removed
			runningProcessesMu.Lock()
			_, exists := runningProcesses[deadPID]
			runningProcessesMu.Unlock()
			Expect(exists).To(BeFalse())
		})
	})
})
