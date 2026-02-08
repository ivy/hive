package github_test

import (
	"context"
	"fmt"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/github"
)

// fakeRunner returns a CommandRunner that executes a shell script echoing
// the given output and exiting with the given code. This avoids calling
// real binaries while exercising real *exec.Cmd.Output / CombinedOutput.
func fakeRunner(output string, exitCode int) github.CommandRunner {
	return func(name string, args ...string) *exec.Cmd {
		// Pass output via env var to avoid shell quoting issues with
		// multi-byte characters (like emoji).
		script := fmt.Sprintf("printf '%%s' \"$FAKE_OUTPUT\"; exit %d", exitCode)
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = append(cmd.Environ(), "FAKE_OUTPUT="+output)
		return cmd
	}
}

// recordingRunner captures the command name and args for assertion,
// then delegates to an inner runner for output.
type recordingRunner struct {
	calls [][]string
	inner github.CommandRunner
}

func (r *recordingRunner) run(name string, args ...string) *exec.Cmd {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.inner(name, args...)
}

var _ = Describe("Client", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("NewClient", func() {
		It("succeeds when gh is on PATH", func() {
			// gh should be available in the test environment via mise
			client, err := github.NewClient()
			Expect(err).NotTo(HaveOccurred())
			Expect(client).NotTo(BeNil())
		})
	})

	Describe("NewClientWithRunner", func() {
		It("returns a client with the given runner", func() {
			runner := fakeRunner("", 0)
			client := github.NewClientWithRunner(runner)
			Expect(client).NotTo(BeNil())
		})
	})

	Describe("FetchIssue", func() {
		It("parses a valid issue response", func() {
			issueJSON := `{"number":143,"title":"feat(hooks): allow bypassing","body":"Issue body","state":"OPEN","url":"https://github.com/ivy/dotfiles/issues/143"}`
			client := github.NewClientWithRunner(fakeRunner(issueJSON, 0))

			issue, err := client.FetchIssue(ctx, "ivy/dotfiles", 143)
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.Number).To(Equal(143))
			Expect(issue.Title).To(Equal("feat(hooks): allow bypassing"))
			Expect(issue.Body).To(Equal("Issue body"))
			Expect(issue.State).To(Equal("OPEN"))
			Expect(issue.URL).To(Equal("https://github.com/ivy/dotfiles/issues/143"))
		})

		It("passes the correct arguments to gh", func() {
			issueJSON := `{"number":143,"title":"t","body":"b","state":"OPEN","url":"u"}`
			rec := &recordingRunner{inner: fakeRunner(issueJSON, 0)}
			client := github.NewClientWithRunner(rec.run)

			_, err := client.FetchIssue(ctx, "ivy/dotfiles", 143)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.calls).To(HaveLen(1))
			Expect(rec.calls[0]).To(Equal([]string{
				"gh", "issue", "view", "143",
				"--repo", "ivy/dotfiles",
				"--json", "number,title,body,state,url",
			}))
		})

		It("returns an error when gh fails", func() {
			client := github.NewClientWithRunner(fakeRunner("", 1))

			_, err := client.FetchIssue(ctx, "ivy/dotfiles", 143)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("gh issue view"))
		})

		It("returns an error for invalid JSON", func() {
			client := github.NewClientWithRunner(fakeRunner("not json", 0))

			_, err := client.FetchIssue(ctx, "ivy/dotfiles", 143)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parsing issue JSON"))
		})
	})

	Describe("ReadyItems", func() {
		It("filters for items with Ready status", func() {
			resp := `{"items":[
				{"id":"PVTI_abc","title":"feat: something","status":"Ready 🤖","content":{"number":143,"repository":"ivy/dotfiles","type":"Issue"}},
				{"id":"PVTI_def","title":"fix: other","status":"In Progress","content":{"number":200,"repository":"ivy/hive","type":"Issue"}},
				{"id":"PVTI_ghi","title":"chore: ready thing","status":"Ready 🤖","content":{"number":50,"repository":"ivy/scripts","type":"Issue"}}
			]}`
			client := github.NewClientWithRunner(fakeRunner(resp, 0))

			items, err := client.ReadyItems(ctx, "42")
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(items[0].ID).To(Equal("PVTI_abc"))
			Expect(items[0].Number).To(Equal(143))
			Expect(items[0].Repo).To(Equal("ivy/dotfiles"))
			Expect(items[0].Status).To(Equal("Ready 🤖"))
			Expect(items[0].Type).To(Equal("Issue"))
			Expect(items[1].ID).To(Equal("PVTI_ghi"))
			Expect(items[1].Number).To(Equal(50))
			Expect(items[1].Repo).To(Equal("ivy/scripts"))
		})

		It("returns empty slice when no items are ready", func() {
			resp := `{"items":[{"id":"PVTI_def","title":"fix: other","status":"In Progress","content":{"number":200,"repository":"ivy/hive","type":"Issue"}}]}`
			client := github.NewClientWithRunner(fakeRunner(resp, 0))

			items, err := client.ReadyItems(ctx, "42")
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("returns empty slice when item list is empty", func() {
			client := github.NewClientWithRunner(fakeRunner(`{"items":[]}`, 0))

			items, err := client.ReadyItems(ctx, "42")
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("passes the correct arguments to gh", func() {
			rec := &recordingRunner{inner: fakeRunner(`{"items":[]}`, 0)}
			client := github.NewClientWithRunner(rec.run)

			_, err := client.ReadyItems(ctx, "42")
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.calls).To(HaveLen(1))
			Expect(rec.calls[0]).To(Equal([]string{
				"gh", "project", "item-list", "42",
				"--owner", "@me",
				"--format", "json",
			}))
		})

		It("returns an error when gh fails", func() {
			client := github.NewClientWithRunner(fakeRunner("", 1))

			_, err := client.ReadyItems(ctx, "42")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("gh project item-list"))
		})

		It("returns an error for invalid JSON", func() {
			client := github.NewClientWithRunner(fakeRunner("{bad", 0))

			_, err := client.ReadyItems(ctx, "42")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parsing project items JSON"))
		})
	})

	Describe("MoveToInProgress", func() {
		It("passes the correct arguments to gh", func() {
			rec := &recordingRunner{inner: fakeRunner("", 0)}
			client := github.NewClientWithRunner(rec.run)
			client.StatusFieldID = "PVTSSF_status"
			client.InProgressOptionID = "opt_inprogress"

			err := client.MoveToInProgress(ctx, "PVT_proj", "PVTI_item")
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.calls).To(HaveLen(1))
			Expect(rec.calls[0]).To(Equal([]string{
				"gh", "project", "item-edit",
				"--project-id", "PVT_proj",
				"--id", "PVTI_item",
				"--field-id", "PVTSSF_status",
				"--single-select-option-id", "opt_inprogress",
			}))
		})

		It("returns an error when gh fails", func() {
			client := github.NewClientWithRunner(fakeRunner("some error", 1))
			client.StatusFieldID = "PVTSSF_status"
			client.InProgressOptionID = "opt_inprogress"

			err := client.MoveToInProgress(ctx, "PVT_proj", "PVTI_item")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("gh project item-edit"))
		})
	})

	Describe("MoveToInReview", func() {
		It("passes the correct arguments to gh", func() {
			rec := &recordingRunner{inner: fakeRunner("", 0)}
			client := github.NewClientWithRunner(rec.run)
			client.StatusFieldID = "PVTSSF_status"
			client.InReviewOptionID = "opt_inreview"

			err := client.MoveToInReview(ctx, "PVT_proj", "PVTI_item")
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.calls).To(HaveLen(1))
			Expect(rec.calls[0]).To(Equal([]string{
				"gh", "project", "item-edit",
				"--project-id", "PVT_proj",
				"--id", "PVTI_item",
				"--field-id", "PVTSSF_status",
				"--single-select-option-id", "opt_inreview",
			}))
		})

		It("returns an error when gh fails", func() {
			client := github.NewClientWithRunner(fakeRunner("some error", 1))
			client.StatusFieldID = "PVTSSF_status"
			client.InReviewOptionID = "opt_inreview"

			err := client.MoveToInReview(ctx, "PVT_proj", "PVTI_item")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("gh project item-edit"))
		})
	})

	Describe("PushBranch", func() {
		It("passes the correct arguments to git", func() {
			rec := &recordingRunner{inner: fakeRunner("", 0)}
			client := github.NewClientWithRunner(rec.run)

			err := client.PushBranch(ctx, "/tmp/hive/dotfiles-143", "hive/dotfiles-143-1738900000")
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.calls).To(HaveLen(1))
			Expect(rec.calls[0]).To(Equal([]string{
				"git", "-C", "/tmp/hive/dotfiles-143",
				"push", "origin", "hive/dotfiles-143-1738900000",
			}))
		})

		It("returns an error when git push fails", func() {
			client := github.NewClientWithRunner(fakeRunner("rejected", 1))

			err := client.PushBranch(ctx, "/tmp/hive/dotfiles-143", "hive/dotfiles-143-1738900000")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("git push"))
		})
	})

	Describe("CreatePR", func() {
		It("parses a valid PR response", func() {
			prJSON := `{"number":42,"title":"feat(hooks): allow bypassing","url":"https://github.com/ivy/dotfiles/pull/42","headRefName":"hive/dotfiles-143-1738900000"}`
			client := github.NewClientWithRunner(fakeRunner(prJSON, 0))

			pr, err := client.CreatePR(ctx, "ivy/dotfiles", "hive/dotfiles-143-1738900000", "feat(hooks): allow bypassing", "PR body")
			Expect(err).NotTo(HaveOccurred())
			Expect(pr.Number).To(Equal(42))
			Expect(pr.Title).To(Equal("feat(hooks): allow bypassing"))
			Expect(pr.URL).To(Equal("https://github.com/ivy/dotfiles/pull/42"))
			Expect(pr.Branch).To(Equal("hive/dotfiles-143-1738900000"))
		})

		It("passes the correct arguments to gh", func() {
			prJSON := `{"number":42,"title":"t","url":"u","headRefName":"b"}`
			rec := &recordingRunner{inner: fakeRunner(prJSON, 0)}
			client := github.NewClientWithRunner(rec.run)

			_, err := client.CreatePR(ctx, "ivy/dotfiles", "hive/dotfiles-143", "the title", "the body")
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.calls).To(HaveLen(1))
			Expect(rec.calls[0]).To(Equal([]string{
				"gh", "pr", "create",
				"--repo", "ivy/dotfiles",
				"--head", "hive/dotfiles-143",
				"--title", "the title",
				"--body", "the body",
				"--json", "number,title,url,headRefName",
			}))
		})

		It("returns an error when gh fails", func() {
			client := github.NewClientWithRunner(fakeRunner("", 1))

			_, err := client.CreatePR(ctx, "ivy/dotfiles", "branch", "title", "body")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("gh pr create"))
		})

		It("returns an error for invalid JSON", func() {
			client := github.NewClientWithRunner(fakeRunner("not json", 0))

			_, err := client.CreatePR(ctx, "ivy/dotfiles", "branch", "title", "body")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parsing PR JSON"))
		})
	})
})
