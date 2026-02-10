package workspace_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/workspace"
)

// initBareRepo creates a temp dir with a git repo containing an initial commit.
// Returns the repo path. The caller should defer os.RemoveAll(path).
func initBareRepo(dir string) string {
	repoPath := filepath.Join(dir, "test-repo")
	Expect(os.MkdirAll(repoPath, 0o755)).To(Succeed())

	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "cmd %v failed: %s", args, string(out))
	}

	return repoPath
}

var _ = Describe("Workspace", func() {
	var (
		ctx        context.Context
		tmpDir     string
		repoPath   string
		origXDG    string
		hadOrigXDG bool
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "hive-workspace-test-*")
		Expect(err).NotTo(HaveOccurred())

		// Isolate workspaces to temp dir via XDG_DATA_HOME.
		origXDG, hadOrigXDG = os.LookupEnv("XDG_DATA_HOME")
		os.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "xdg-data"))

		repoPath = initBareRepo(tmpDir)
	})

	AfterEach(func() {
		if hadOrigXDG {
			os.Setenv("XDG_DATA_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_DATA_HOME")
		}
		os.RemoveAll(tmpDir)
	})

	Describe("Create", func() {
		var ws *workspace.Workspace

		AfterEach(func() {
			if ws != nil {
				os.RemoveAll(ws.Path)
				// Clean up worktree and branch in the repo.
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
				ws = nil
			}
		})

		It("creates a worktree directory", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(ws.Path).To(BeADirectory())
		})

		It("creates the .hive metadata directory", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(filepath.Join(ws.Path, ".hive")).To(BeADirectory())
		})

		It("writes repo metadata", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "repo"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("ivy/dotfiles"))
		})

		It("writes issue-number metadata", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "issue-number"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("132"))
		})

		It("writes the session-id matching the provided UUID", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "session-id"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal(id))
		})

		It("sets initial status to prepared", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "status"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("prepared"))
		})

		It("uses the UUID as branch suffix", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(ws.Branch).To(Equal("hive/" + id))
		})

		It("puts the workspace under the XDG workspaces directory", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(ws.Path).To(Equal(filepath.Join(workspace.BaseDir(), id)))
		})

		It("populates all struct fields", func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 132, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(ws.RepoPath).To(Equal(repoPath))
			Expect(ws.Repo).To(Equal("ivy/dotfiles"))
			Expect(ws.IssueNumber).To(Equal(132))
			Expect(ws.SessionID).To(Equal(id))
			Expect(ws.Status).To(Equal(workspace.StatusPrepared))
		})

		It("returns an error for a nonexistent repo path", func() {
			_, err := workspace.Create(ctx, "/nonexistent/repo", "ivy/dotfiles", 1, uuid.New().String())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("git worktree add"))
		})
	})

	Describe("Load", func() {
		var ws *workspace.Workspace

		BeforeEach(func() {
			var err error
			id := uuid.New().String()
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 42, id)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if ws != nil {
				os.RemoveAll(ws.Path)
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
			}
		})

		It("reconstructs the workspace from metadata", func() {
			loaded, err := workspace.Load(ctx, ws.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.Repo).To(Equal("ivy/dotfiles"))
			Expect(loaded.IssueNumber).To(Equal(42))
			Expect(loaded.SessionID).To(Equal(ws.SessionID))
			Expect(loaded.Status).To(Equal(workspace.StatusPrepared))
			Expect(loaded.Branch).To(Equal(ws.Branch))
		})

		It("resolves the repo path via git", func() {
			loaded, err := workspace.Load(ctx, ws.Path)
			Expect(err).NotTo(HaveOccurred())
			// RepoPath should resolve to the same directory (canonicalized).
			resolvedExpected, _ := filepath.EvalSymlinks(repoPath)
			resolvedActual, _ := filepath.EvalSymlinks(loaded.RepoPath)
			Expect(resolvedActual).To(Equal(resolvedExpected))
		})

		It("returns an error for a directory without .hive", func() {
			_, err := workspace.Load(ctx, tmpDir)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Remove", func() {
		It("removes the worktree directory and branch", func() {
			ws, err := workspace.Create(ctx, repoPath, "ivy/dotfiles", 99, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())

			wsPath := ws.Path
			branch := ws.Branch

			err = workspace.Remove(ctx, ws)
			Expect(err).NotTo(HaveOccurred())

			Expect(wsPath).NotTo(BeADirectory())

			// Verify branch is deleted.
			cmd := exec.Command("git", "branch", "--list", branch)
			cmd.Dir = repoPath
			out, err := cmd.Output()
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(string(out))).To(BeEmpty())
		})
	})

	Describe("ListAll", func() {
		var createdWorkspaces []*workspace.Workspace

		AfterEach(func() {
			for _, ws := range createdWorkspaces {
				os.RemoveAll(ws.Path)
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
			}
			createdWorkspaces = nil
		})

		It("returns workspaces that exist in the base directory", func() {
			ws1, err := workspace.Create(ctx, repoPath, "ivy/dotfiles", 10, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())
			createdWorkspaces = append(createdWorkspaces, ws1)

			ws2, err := workspace.Create(ctx, repoPath, "ivy/dotfiles", 20, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())
			createdWorkspaces = append(createdWorkspaces, ws2)

			list, err := workspace.ListAll(ctx)
			Expect(err).NotTo(HaveOccurred())

			paths := make([]string, len(list))
			for i, ws := range list {
				paths[i] = ws.Path
			}
			Expect(paths).To(ContainElement(ws1.Path))
			Expect(paths).To(ContainElement(ws2.Path))
		})
	})

	Describe("SetStatus", func() {
		var ws *workspace.Workspace

		BeforeEach(func() {
			var err error
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 50, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if ws != nil {
				os.RemoveAll(ws.Path)
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
			}
		})

		It("writes the status to .hive/status", func() {
			err := workspace.SetStatus(ws, workspace.StatusRunning)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "status"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("running"))
		})

		It("updates the workspace struct", func() {
			err := workspace.SetStatus(ws, workspace.StatusFailed)
			Expect(err).NotTo(HaveOccurred())
			Expect(ws.Status).To(Equal(workspace.StatusFailed))
		})
	})

	Describe("WriteIssueData", func() {
		var ws *workspace.Workspace

		BeforeEach(func() {
			var err error
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 60, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if ws != nil {
				os.RemoveAll(ws.Path)
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
			}
		})

		It("writes JSON data to .hive/issue.json", func() {
			issueData := map[string]interface{}{
				"number": 60,
				"title":  "Test issue",
				"body":   "Fix all the things",
			}
			data, err := json.Marshal(issueData)
			Expect(err).NotTo(HaveOccurred())

			err = workspace.WriteIssueData(ws, data)
			Expect(err).NotTo(HaveOccurred())

			read, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "issue.json"))
			Expect(err).NotTo(HaveOccurred())
			Expect(read).To(Equal(data))
		})
	})

	Describe("WritePrompt", func() {
		var ws *workspace.Workspace

		BeforeEach(func() {
			var err error
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 70, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if ws != nil {
				os.RemoveAll(ws.Path)
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
			}
		})

		It("writes content to .hive/prompt.md", func() {
			content := "# Fix the bug\n\nPlease fix the authentication issue."
			err := workspace.WritePrompt(ws, content)
			Expect(err).NotTo(HaveOccurred())

			read, err := os.ReadFile(filepath.Join(ws.Path, ".hive", "prompt.md"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(read)).To(Equal(content))
		})
	})

	Describe("HasUncommittedChanges", func() {
		var ws *workspace.Workspace

		BeforeEach(func() {
			var err error
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 90, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if ws != nil {
				os.RemoveAll(ws.Path)
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
			}
		})

		It("returns false when no files are changed", func() {
			has, err := workspace.HasUncommittedChanges(ctx, ws)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeFalse())
		})

		It("returns true when a tracked file is modified", func() {
			err := os.WriteFile(filepath.Join(ws.Path, "new-file.txt"), []byte("hello"), 0o644)
			Expect(err).NotTo(HaveOccurred())
			has, err := workspace.HasUncommittedChanges(ctx, ws)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeTrue())
		})

		It("ignores .hive/ metadata files", func() {
			// .hive/ files are created by Create, but should not count.
			// Verify that the only untracked files are under .hive/.
			has, err := workspace.HasUncommittedChanges(ctx, ws)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeFalse())
		})
	})

	Describe("CommitAll", func() {
		var ws *workspace.Workspace

		BeforeEach(func() {
			var err error
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 91, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if ws != nil {
				os.RemoveAll(ws.Path)
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
			}
		})

		It("commits new files and leaves a clean worktree", func() {
			err := os.WriteFile(filepath.Join(ws.Path, "change.txt"), []byte("data"), 0o644)
			Expect(err).NotTo(HaveOccurred())

			err = workspace.CommitAll(ctx, ws, "test commit")
			Expect(err).NotTo(HaveOccurred())

			// Worktree should be clean (except .hive/).
			has, err := workspace.HasUncommittedChanges(ctx, ws)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeFalse())

			// Verify the commit message.
			cmd := exec.Command("git", "log", "-1", "--format=%s")
			cmd.Dir = ws.Path
			out, err := cmd.Output()
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(string(out))).To(Equal("test commit"))
		})

		It("does not commit .hive/ metadata", func() {
			err := os.WriteFile(filepath.Join(ws.Path, "real-change.txt"), []byte("x"), 0o644)
			Expect(err).NotTo(HaveOccurred())

			err = workspace.CommitAll(ctx, ws, "test commit")
			Expect(err).NotTo(HaveOccurred())

			// .hive/ files should not appear in the commit.
			cmd := exec.Command("git", "show", "--name-only", "--format=")
			cmd.Dir = ws.Path
			out, err := cmd.Output()
			Expect(err).NotTo(HaveOccurred())
			for _, line := range strings.Split(string(out), "\n") {
				Expect(line).NotTo(HavePrefix(".hive/"))
			}
		})

		It("returns nil when there is nothing to commit", func() {
			err := workspace.CommitAll(ctx, ws, "empty commit")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("ReadSessionID", func() {
		var ws *workspace.Workspace

		BeforeEach(func() {
			var err error
			ws, err = workspace.Create(ctx, repoPath, "ivy/dotfiles", 80, uuid.New().String())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if ws != nil {
				os.RemoveAll(ws.Path)
				cmd := exec.Command("git", "worktree", "prune")
				cmd.Dir = repoPath
				_ = cmd.Run()
				cmd = exec.Command("git", "branch", "-D", ws.Branch)
				cmd.Dir = repoPath
				_ = cmd.Run()
			}
		})

		It("reads the session-id written by Create", func() {
			sessionID, err := workspace.ReadSessionID(ws)
			Expect(err).NotTo(HaveOccurred())
			Expect(sessionID).To(Equal(ws.SessionID))
		})

		It("returns an error for a missing session-id file", func() {
			// Remove the session-id file.
			os.Remove(filepath.Join(ws.Path, ".hive", "session-id"))
			_, err := workspace.ReadSessionID(ws)
			Expect(err).To(HaveOccurred())
		})
	})
})
