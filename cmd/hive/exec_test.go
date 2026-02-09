package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/workspace"
)

var _ = Describe("buildNewClaudeCommand", func() {
	var ws *workspace.Workspace

	BeforeEach(func() {
		ws = &workspace.Workspace{
			SessionID: "test-session-123",
		}
	})

	It("includes --append-system-prompt with the agent system prompt", func() {
		cmd, err := buildNewClaudeCommand(ws, "sonnet", "implement the feature")
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd).To(ContainElements("--append-system-prompt", agentSystemPrompt))
	})

	It("includes --session-id from the workspace", func() {
		cmd, err := buildNewClaudeCommand(ws, "sonnet", "implement the feature")
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd).To(ContainElements("--session-id", "test-session-123"))
	})

	It("includes --model with the specified model", func() {
		cmd, err := buildNewClaudeCommand(ws, "opus", "implement the feature")
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd).To(ContainElements("--model", "opus"))
	})

	It("ends with the prompt as the last argument", func() {
		cmd, err := buildNewClaudeCommand(ws, "sonnet", "do the thing")
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd[len(cmd)-1]).To(Equal("do the thing"))
	})
})

var _ = Describe("buildClaudeCommand", func() {
	Context("when resuming a session", func() {
		var ws *workspace.Workspace

		BeforeEach(func() {
			ws = &workspace.Workspace{
				SessionID: "existing-session",
			}
		})

		It("includes --append-system-prompt in the resume command", func() {
			// buildClaudeCommand reads the session-id file when resuming,
			// but falls back to buildNewClaudeCommand if it can't.
			// Since we don't have a real workspace on disk, it falls back.
			cmd, err := buildClaudeCommand(ws, "continue working", "sonnet")
			Expect(err).NotTo(HaveOccurred())
			Expect(cmd).To(ContainElements("--append-system-prompt", agentSystemPrompt))
		})
	})
})

var _ = Describe("commit retry constants", func() {
	It("allows up to 3 retries", func() {
		Expect(maxCommitRetries).To(Equal(3))
	})

	It("has a non-empty commit nudge message", func() {
		Expect(commitNudge).To(ContainSubstring("/commit"))
	})

	It("has a non-empty agent system prompt", func() {
		Expect(agentSystemPrompt).To(ContainSubstring("/commit"))
	})
})
