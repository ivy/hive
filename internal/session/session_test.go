package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/session"
)

var _ = Describe("Session", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "hive-session-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	Describe("DataDir", func() {
		It("uses XDG_DATA_HOME when set", func() {
			original := os.Getenv("XDG_DATA_HOME")
			defer os.Setenv("XDG_DATA_HOME", original)

			os.Setenv("XDG_DATA_HOME", "/custom/data")
			Expect(session.DataDir()).To(Equal("/custom/data/hive"))
		})

		It("falls back to ~/.local/share/hive", func() {
			original := os.Getenv("XDG_DATA_HOME")
			defer os.Setenv("XDG_DATA_HOME", original)

			os.Unsetenv("XDG_DATA_HOME")
			home, err := os.UserHomeDir()
			Expect(err).NotTo(HaveOccurred())
			Expect(session.DataDir()).To(Equal(filepath.Join(home, ".local", "share", "hive")))
		})
	})

	Describe("Create", func() {
		It("writes a session JSON file", func() {
			s := &session.Session{
				ID:     "abc-123",
				Status: session.StatusDispatching,
			}
			err := session.Create(tmpDir, s)
			Expect(err).NotTo(HaveOccurred())

			path := filepath.Join(tmpDir, "sessions", "abc-123.json")
			Expect(path).To(BeAnExistingFile())
		})

		It("creates the sessions directory if missing", func() {
			s := &session.Session{
				ID:     "abc-456",
				Status: session.StatusPrepared,
			}
			err := session.Create(tmpDir, s)
			Expect(err).NotTo(HaveOccurred())

			Expect(filepath.Join(tmpDir, "sessions")).To(BeADirectory())
		})

		It("marshals all fields correctly", func() {
			now := time.Now().Truncate(time.Second)
			s := &session.Session{
				ID:    "full-test",
				Ref:   "issues/42",
				Repo:  "ivy/dotfiles",
				Title: "Fix the thing",
				Prompt: "Please fix it",
				SourceMetadata: map[string]string{
					"issue_number": "42",
					"author":       "ivy",
				},
				Status:       session.StatusRunning,
				CreatedAt:    now,
				PollInstance: "poll-001",
			}
			err := session.Create(tmpDir, s)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(filepath.Join(tmpDir, "sessions", "full-test.json"))
			Expect(err).NotTo(HaveOccurred())

			var loaded session.Session
			Expect(json.Unmarshal(data, &loaded)).To(Succeed())
			Expect(loaded.ID).To(Equal("full-test"))
			Expect(loaded.Ref).To(Equal("issues/42"))
			Expect(loaded.Repo).To(Equal("ivy/dotfiles"))
			Expect(loaded.Title).To(Equal("Fix the thing"))
			Expect(loaded.Prompt).To(Equal("Please fix it"))
			Expect(loaded.SourceMetadata).To(HaveKeyWithValue("issue_number", "42"))
			Expect(loaded.SourceMetadata).To(HaveKeyWithValue("author", "ivy"))
			Expect(loaded.Status).To(Equal(session.StatusRunning))
			Expect(loaded.CreatedAt).To(BeTemporally("~", now, time.Second))
			Expect(loaded.PollInstance).To(Equal("poll-001"))
		})
	})

	Describe("Load", func() {
		It("reads back a created session", func() {
			s := &session.Session{
				ID:     "load-test",
				Repo:   "ivy/hive",
				Status: session.StatusPrepared,
			}
			Expect(session.Create(tmpDir, s)).To(Succeed())

			loaded, err := session.Load(tmpDir, "load-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.ID).To(Equal("load-test"))
			Expect(loaded.Repo).To(Equal("ivy/hive"))
			Expect(loaded.Status).To(Equal(session.StatusPrepared))
		})

		It("returns an error for a nonexistent session", func() {
			_, err := session.Load(tmpDir, "does-not-exist")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListAll", func() {
		It("returns all sessions", func() {
			s1 := &session.Session{ID: "list-1", Status: session.StatusRunning}
			s2 := &session.Session{ID: "list-2", Status: session.StatusFailed}
			Expect(session.Create(tmpDir, s1)).To(Succeed())
			Expect(session.Create(tmpDir, s2)).To(Succeed())

			list, err := session.ListAll(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(2))

			ids := []string{list[0].ID, list[1].ID}
			Expect(ids).To(ContainElement("list-1"))
			Expect(ids).To(ContainElement("list-2"))
		})

		It("returns empty slice when sessions dir does not exist", func() {
			list, err := session.ListAll(filepath.Join(tmpDir, "nonexistent"))
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(BeEmpty())
		})

		It("skips malformed files", func() {
			s := &session.Session{ID: "good-one", Status: session.StatusPrepared}
			Expect(session.Create(tmpDir, s)).To(Succeed())

			// Write a malformed JSON file.
			badPath := filepath.Join(tmpDir, "sessions", "bad.json")
			Expect(os.WriteFile(badPath, []byte("not json{{{"), 0o644)).To(Succeed())

			list, err := session.ListAll(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].ID).To(Equal("good-one"))
		})
	})

	Describe("SetStatus", func() {
		It("updates the status in the JSON file", func() {
			s := &session.Session{ID: "status-test", Status: session.StatusPrepared}
			Expect(session.Create(tmpDir, s)).To(Succeed())

			err := session.SetStatus(tmpDir, "status-test", session.StatusRunning)
			Expect(err).NotTo(HaveOccurred())

			loaded, err := session.Load(tmpDir, "status-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.Status).To(Equal(session.StatusRunning))
		})

		It("returns an error for a nonexistent session", func() {
			err := session.SetStatus(tmpDir, "missing", session.StatusFailed)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Remove", func() {
		It("deletes the session file", func() {
			s := &session.Session{ID: "remove-test", Status: session.StatusStopped}
			Expect(session.Create(tmpDir, s)).To(Succeed())

			err := session.Remove(tmpDir, "remove-test")
			Expect(err).NotTo(HaveOccurred())

			path := filepath.Join(tmpDir, "sessions", "remove-test.json")
			Expect(path).NotTo(BeAnExistingFile())
		})

		It("is idempotent for nonexistent sessions", func() {
			err := session.Remove(tmpDir, "never-existed")
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
