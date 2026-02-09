package prdraft_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/jail"
	"github.com/ivy/hive/internal/prdraft"
	"github.com/ivy/hive/internal/workspace"
)

// fakeJail implements jail.Jail with canned RunCapture responses.
type fakeJail struct {
	// captureResponses is a sequence of responses for RunCapture calls.
	captureResponses []fakeResponse
	// capturedOpts records the RunOpts from each RunCapture call.
	capturedOpts []jail.RunOpts
	callCount    int
}

type fakeResponse struct {
	data []byte
	err  error
}

func (f *fakeJail) Run(_ context.Context, _ jail.RunOpts) error {
	return fmt.Errorf("Run not expected in prdraft tests")
}

func (f *fakeJail) RunCapture(_ context.Context, opts jail.RunOpts) ([]byte, error) {
	f.capturedOpts = append(f.capturedOpts, opts)
	idx := f.callCount
	f.callCount++
	if idx >= len(f.captureResponses) {
		return nil, fmt.Errorf("unexpected RunCapture call #%d", idx)
	}
	return f.captureResponses[idx].data, f.captureResponses[idx].err
}

func makeResponse(sessionID string, content *prdraft.PRContent) []byte {
	resp := map[string]any{
		"session_id": sessionID,
	}
	if content != nil {
		resp["structured_output"] = content
	}
	data, _ := json.Marshal(resp)
	return data
}

func testWorkspace() *workspace.Workspace {
	return &workspace.Workspace{
		Path:        "/tmp/hive/ws-test",
		RepoPath:    "/home/ivy/src/testrepo",
		Repo:        "ivy/testrepo",
		IssueNumber: 42,
		Branch:      "hive/testrepo-42-123",
		SessionID:   "abc-session-123",
	}
}

var _ = Describe("Drafter", func() {
	var (
		fj     *fakeJail
		ws     *workspace.Workspace
		params prdraft.DraftParams
	)

	BeforeEach(func() {
		fj = &fakeJail{}
		ws = testWorkspace()
		params = prdraft.DraftParams{
			Workspace: ws,
			Model:     "sonnet",
			Resume:    true,
		}
	})

	Describe("Draft", func() {
		Context("happy path", func() {
			BeforeEach(func() {
				fj.captureResponses = []fakeResponse{
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "feat(auth): add login flow :sparkles:",
						Body:  "## Why\nUsers need to log in.",
					})},
				}
			})

			It("returns PR content on first attempt", func() {
				drafter := prdraft.New(fj)
				content, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())
				Expect(content.Title).To(Equal("feat(auth): add login flow :sparkles:"))
				Expect(content.Body).To(ContainSubstring("Users need to log in."))
			})

			It("appends the footer with Closes directive", func() {
				drafter := prdraft.New(fj)
				content, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())
				Expect(content.Body).To(ContainSubstring("Generated with [Hive](https://github.com/ivy/hive) | Closes #42"))
			})

			It("calls RunCapture exactly once", func() {
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())
				Expect(fj.callCount).To(Equal(1))
			})
		})

		Context("retry on missing structured_output", func() {
			BeforeEach(func() {
				fj.captureResponses = []fakeResponse{
					// First attempt: no structured output
					{data: makeResponse("sess-1", nil)},
					// Second attempt: success
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "fix(api): handle timeout :bug:",
						Body:  "## Why\nTimeouts were unhandled.",
					})},
				}
			})

			It("succeeds on second attempt", func() {
				drafter := prdraft.New(fj)
				content, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())
				Expect(content.Title).To(Equal("fix(api): handle timeout :bug:"))
				Expect(fj.callCount).To(Equal(2))
			})

			It("resumes with the session ID from first response", func() {
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())

				// Second call should have --resume with session ID
				secondOpts := fj.capturedOpts[1]
				Expect(secondOpts.Command).To(ContainElement("--resume"))
				Expect(secondOpts.Command).To(ContainElement("sess-1"))
			})
		})

		Context("retry exhaustion", func() {
			BeforeEach(func() {
				fj.captureResponses = []fakeResponse{
					{data: makeResponse("sess-1", nil)},
					{data: makeResponse("sess-1", nil)},
					{data: makeResponse("sess-1", nil)},
				}
			})

			It("returns an error after 3 failed attempts", func() {
				drafter := prdraft.New(fj)
				content, err := drafter.Draft(context.Background(), params)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("3 attempts"))
				Expect(content).To(BeNil())
				Expect(fj.callCount).To(Equal(3))
			})
		})

		Context("argument building", func() {
			BeforeEach(func() {
				fj.captureResponses = []fakeResponse{
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "feat: something :sparkles:",
						Body:  "body",
					})},
				}
			})

			It("includes --resume when Resume is true", func() {
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())

				cmd := fj.capturedOpts[0].Command
				Expect(cmd).To(ContainElement("--resume"))
				Expect(cmd).To(ContainElement("abc-session-123"))
			})

			It("omits --resume when Resume is false", func() {
				params.Resume = false
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())

				cmd := fj.capturedOpts[0].Command
				Expect(cmd).NotTo(ContainElement("--resume"))
			})

			It("includes required claude flags", func() {
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())

				cmd := fj.capturedOpts[0].Command
				Expect(cmd[0]).To(Equal("claude"))
				Expect(cmd).To(ContainElement("-p"))
				Expect(cmd).To(ContainElement("--output-format"))
				Expect(cmd).To(ContainElement("json"))
				Expect(cmd).To(ContainElement("--json-schema"))
				Expect(cmd).To(ContainElement("--tools"))
				Expect(cmd).To(ContainElement("--allowedTools"))
				Expect(cmd).To(ContainElement("--model"))
				Expect(cmd).To(ContainElement("sonnet"))
			})

			It("passes the workspace to RunOpts", func() {
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).NotTo(HaveOccurred())

				Expect(fj.capturedOpts[0].Workspace).To(Equal(ws))
			})
		})

		Context("output parsing", func() {
			It("handles malformed JSON", func() {
				fj.captureResponses = []fakeResponse{
					{data: []byte("not json")},
					{data: []byte("still not json")},
					{data: []byte("nope")},
				}
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).To(HaveOccurred())
			})

			It("treats empty title as missing structured output", func() {
				fj.captureResponses = []fakeResponse{
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "",
						Body:  "has body",
					})},
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "",
						Body:  "has body",
					})},
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "",
						Body:  "has body",
					})},
				}
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("3 attempts"))
			})

			It("treats empty body as missing structured output", func() {
				fj.captureResponses = []fakeResponse{
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "has title",
						Body:  "",
					})},
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "has title",
						Body:  "",
					})},
					{data: makeResponse("sess-1", &prdraft.PRContent{
						Title: "has title",
						Body:  "",
					})},
				}
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("3 attempts"))
			})
		})

		Context("when RunCapture fails", func() {
			It("returns the error immediately", func() {
				fj.captureResponses = []fakeResponse{
					{err: fmt.Errorf("jail exploded")},
				}
				drafter := prdraft.New(fj)
				_, err := drafter.Draft(context.Background(), params)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("jail exploded"))
			})
		})
	})

	Describe("Fallback", func() {
		It("returns mechanical title", func() {
			content := prdraft.Fallback(42)
			Expect(content.Title).To(Equal("hive: implement #42"))
		})

		It("returns body with Closes directive", func() {
			content := prdraft.Fallback(42)
			Expect(content.Body).To(ContainSubstring("Closes #42"))
		})
	})

	Describe("prompt rendering", func() {
		BeforeEach(func() {
			fj.captureResponses = []fakeResponse{
				{data: makeResponse("sess-1", &prdraft.PRContent{
					Title: "feat: test :sparkles:",
					Body:  "body",
				})},
			}
		})

		It("includes the issue number in the prompt", func() {
			drafter := prdraft.New(fj)
			_, err := drafter.Draft(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())

			// The prompt is the last element in Command
			cmd := fj.capturedOpts[0].Command
			prompt := cmd[len(cmd)-1]
			Expect(prompt).To(ContainSubstring("#42"))
		})

		It("includes the repo name in the prompt", func() {
			drafter := prdraft.New(fj)
			_, err := drafter.Draft(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())

			cmd := fj.capturedOpts[0].Command
			prompt := cmd[len(cmd)-1]
			Expect(prompt).To(ContainSubstring("ivy/testrepo"))
		})

		It("instructs Claude not to include Closes directive", func() {
			drafter := prdraft.New(fj)
			_, err := drafter.Draft(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())

			cmd := fj.capturedOpts[0].Command
			prompt := cmd[len(cmd)-1]
			Expect(strings.ToLower(prompt)).To(ContainSubstring("do not include"))
			Expect(prompt).To(ContainSubstring("Closes"))
		})
	})
})
