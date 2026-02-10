package ghprojects_test

import (
	"context"
	"fmt"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/github"
	"github.com/ivy/hive/internal/source/ghprojects"
)

// fakeRunner returns a CommandRunner that echoes the given output and exits
// with the given code.
func fakeRunner(output string, exitCode int) github.CommandRunner {
	return func(name string, args ...string) *exec.Cmd {
		script := fmt.Sprintf("printf '%%s' \"$FAKE_OUTPUT\"; exit %d", exitCode)
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = append(cmd.Environ(), "FAKE_OUTPUT="+output)
		return cmd
	}
}

// sequenceRunner returns a CommandRunner that returns different outputs
// for successive calls.
func sequenceRunner(outputs []string, exitCodes []int) github.CommandRunner {
	idx := 0
	return func(name string, args ...string) *exec.Cmd {
		i := idx
		idx++
		script := fmt.Sprintf("printf '%%s' \"$FAKE_OUTPUT\"; exit %d", exitCodes[i])
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = append(cmd.Environ(), "FAKE_OUTPUT="+outputs[i])
		return cmd
	}
}

// recordingRunner captures command name and args, then delegates to inner.
type recordingRunner struct {
	calls [][]string
	inner github.CommandRunner
}

func (r *recordingRunner) run(name string, args ...string) *exec.Cmd {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.inner(name, args...)
}

func readyClient(runner github.CommandRunner) *github.Client {
	c := github.NewClientWithRunner(runner)
	c.ReadyStatus = "Ready 🤖"
	c.StatusFieldID = "PVTSSF_status"
	c.InProgressOptionID = "opt_inprogress"
	c.InReviewOptionID = "opt_inreview"
	c.ReadyOptionID = "opt_ready"
	return c
}

var _ = Describe("Adapter", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	graphQLResp := `{"data":{"viewer":{"projectV2":{"items":{"nodes":[
		{"id":"PVTI_abc","content":{"__typename":"Issue","number":43,"title":"feat: something","repository":{"nameWithOwner":"ivy/hive"}}},
		{"id":"PVTI_def","content":{"__typename":"Issue","number":44,"title":"fix: other","repository":{"nameWithOwner":"ivy/hive"}}}
	]}}}}}`

	issueJSON43 := `{"number":43,"title":"feat: something","body":"Do the thing","state":"OPEN","url":"https://github.com/ivy/hive/issues/43","author":{"login":"ivy"}}`
	issueJSON44 := `{"number":44,"title":"fix: other","body":"Fix the thing","state":"OPEN","url":"https://github.com/ivy/hive/issues/44","author":{"login":"ivy"}}`

	Describe("Ready", func() {
		It("returns work items for ready non-draft issues", func() {
			runner := sequenceRunner(
				[]string{graphQLResp, issueJSON43, issueJSON44},
				[]int{0, 0, 0},
			)
			client := readyClient(runner)
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        client,
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			items, err := adapter.Ready(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(items[0].Ref).To(Equal("github:ivy/hive#43"))
			Expect(items[0].Repo).To(Equal("ivy/hive"))
			Expect(items[0].Title).To(Equal("feat: something"))
			Expect(items[0].Prompt).To(Equal("Do the thing"))
			Expect(items[1].Ref).To(Equal("github:ivy/hive#44"))
			Expect(items[1].Prompt).To(Equal("Fix the thing"))
		})

		It("skips draft issues", func() {
			draftResp := `{"data":{"viewer":{"projectV2":{"items":{"nodes":[
				{"id":"PVTI_draft","content":{"__typename":"DraftIssue","title":"draft thing"}},
				{"id":"PVTI_abc","content":{"__typename":"Issue","number":43,"title":"feat: something","repository":{"nameWithOwner":"ivy/hive"}}}
			]}}}}}`
			runner := sequenceRunner(
				[]string{draftResp, issueJSON43},
				[]int{0, 0},
			)
			client := readyClient(runner)
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        client,
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			items, err := adapter.Ready(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Ref).To(Equal("github:ivy/hive#43"))
		})

		It("skips unauthorized authors", func() {
			unauthIssue := `{"number":43,"title":"feat: something","body":"Do the thing","state":"OPEN","url":"https://github.com/ivy/hive/issues/43","author":{"login":"stranger"}}`
			singleResp := `{"data":{"viewer":{"projectV2":{"items":{"nodes":[
				{"id":"PVTI_abc","content":{"__typename":"Issue","number":43,"title":"feat: something","repository":{"nameWithOwner":"ivy/hive"}}}
			]}}}}}`
			runner := sequenceRunner(
				[]string{singleResp, unauthIssue},
				[]int{0, 0},
			)
			client := readyClient(runner)
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        client,
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			items, err := adapter.Ready(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("populates metadata with board_item_id and project_node_id", func() {
			singleResp := `{"data":{"viewer":{"projectV2":{"items":{"nodes":[
				{"id":"PVTI_abc","content":{"__typename":"Issue","number":43,"title":"feat: something","repository":{"nameWithOwner":"ivy/hive"}}}
			]}}}}}`
			runner := sequenceRunner(
				[]string{singleResp, issueJSON43},
				[]int{0, 0},
			)
			client := readyClient(runner)
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        client,
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			items, err := adapter.Ready(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Metadata).To(HaveKeyWithValue("board_item_id", "PVTI_abc"))
			Expect(items[0].Metadata).To(HaveKeyWithValue("project_node_id", "PVT_proj"))
		})
	})

	Describe("Take", func() {
		It("moves item to In Progress via github client", func() {
			// First call: ReadyItems GraphQL, second: FetchIssue, third: MoveToInProgress
			singleResp := `{"data":{"viewer":{"projectV2":{"items":{"nodes":[
				{"id":"PVTI_abc","content":{"__typename":"Issue","number":43,"title":"feat: something","repository":{"nameWithOwner":"ivy/hive"}}}
			]}}}}}`
			rec := &recordingRunner{
				inner: sequenceRunner(
					[]string{singleResp, issueJSON43, ""},
					[]int{0, 0, 0},
				),
			}
			client := readyClient(rec.run)
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        client,
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			_, err := adapter.Ready(ctx)
			Expect(err).NotTo(HaveOccurred())

			err = adapter.Take(ctx, "github:ivy/hive#43")
			Expect(err).NotTo(HaveOccurred())

			// Third call should be the move command
			Expect(rec.calls).To(HaveLen(3))
			Expect(rec.calls[2]).To(Equal([]string{
				"gh", "project", "item-edit",
				"--project-id", "PVT_proj",
				"--id", "PVTI_abc",
				"--field-id", "PVTSSF_status",
				"--single-select-option-id", "opt_inprogress",
			}))
		})

		It("returns error for unknown ref", func() {
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        readyClient(fakeRunner("", 0)),
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			err := adapter.Take(ctx, "github:ivy/hive#999")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown ref"))
		})
	})

	Describe("Complete", func() {
		It("moves item to In Review via github client", func() {
			singleResp := `{"data":{"viewer":{"projectV2":{"items":{"nodes":[
				{"id":"PVTI_abc","content":{"__typename":"Issue","number":43,"title":"feat: something","repository":{"nameWithOwner":"ivy/hive"}}}
			]}}}}}`
			rec := &recordingRunner{
				inner: sequenceRunner(
					[]string{singleResp, issueJSON43, ""},
					[]int{0, 0, 0},
				),
			}
			client := readyClient(rec.run)
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        client,
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			_, err := adapter.Ready(ctx)
			Expect(err).NotTo(HaveOccurred())

			err = adapter.Complete(ctx, "github:ivy/hive#43")
			Expect(err).NotTo(HaveOccurred())

			Expect(rec.calls).To(HaveLen(3))
			Expect(rec.calls[2]).To(Equal([]string{
				"gh", "project", "item-edit",
				"--project-id", "PVT_proj",
				"--id", "PVTI_abc",
				"--field-id", "PVTSSF_status",
				"--single-select-option-id", "opt_inreview",
			}))
		})

		It("returns error for unknown ref", func() {
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        readyClient(fakeRunner("", 0)),
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			err := adapter.Complete(ctx, "github:ivy/hive#999")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown ref"))
		})
	})

	Describe("RegisterItem", func() {
		It("allows Release to work for items not seen by Ready", func() {
			rec := &recordingRunner{
				inner: fakeRunner("", 0),
			}
			client := readyClient(rec.run)
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        client,
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			adapter.RegisterItem("github:ivy/hive#99", "PVTI_registered")

			err := adapter.Release(ctx, "github:ivy/hive#99")
			Expect(err).NotTo(HaveOccurred())

			Expect(rec.calls).To(HaveLen(1))
			Expect(rec.calls[0]).To(Equal([]string{
				"gh", "project", "item-edit",
				"--project-id", "PVT_proj",
				"--id", "PVTI_registered",
				"--field-id", "PVTSSF_status",
				"--single-select-option-id", "opt_ready",
			}))
		})
	})

	Describe("Release", func() {
		It("moves item to Ready via github client", func() {
			singleResp := `{"data":{"viewer":{"projectV2":{"items":{"nodes":[
				{"id":"PVTI_abc","content":{"__typename":"Issue","number":43,"title":"feat: something","repository":{"nameWithOwner":"ivy/hive"}}}
			]}}}}}`
			rec := &recordingRunner{
				inner: sequenceRunner(
					[]string{singleResp, issueJSON43, ""},
					[]int{0, 0, 0},
				),
			}
			client := readyClient(rec.run)
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        client,
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			_, err := adapter.Ready(ctx)
			Expect(err).NotTo(HaveOccurred())

			err = adapter.Release(ctx, "github:ivy/hive#43")
			Expect(err).NotTo(HaveOccurred())

			Expect(rec.calls).To(HaveLen(3))
			Expect(rec.calls[2]).To(Equal([]string{
				"gh", "project", "item-edit",
				"--project-id", "PVT_proj",
				"--id", "PVTI_abc",
				"--field-id", "PVTSSF_status",
				"--single-select-option-id", "opt_ready",
			}))
		})

		It("returns error for unknown ref", func() {
			adapter := ghprojects.NewAdapter(ghprojects.Config{
				Client:        readyClient(fakeRunner("", 0)),
				ProjectNumber: "42",
				ProjectNodeID: "PVT_proj",
				AllowedUsers:  []string{"ivy"},
			})

			err := adapter.Release(ctx, "github:ivy/hive#999")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown ref"))
		})
	})
})
