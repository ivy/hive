package jail_test

import (
	"context"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ivy/hive/internal/jail"
	"github.com/ivy/hive/internal/workspace"
)

// captured holds the command and args recorded by a fake CommandRunner.
type captured struct {
	name string
	args []string
}

// recordingRunner returns a CommandRunner that records the invocation and
// returns a no-op command (echo) so cmd.Run() succeeds without side effects.
func recordingRunner(c *captured) jail.CommandRunner {
	return func(name string, args ...string) *exec.Cmd {
		c.name = name
		c.args = args
		return exec.Command("echo", "noop")
	}
}

var _ = Describe("Jail", func() {
	Describe("New", func() {
		It("returns a Jail for the systemd-run backend", func() {
			j, err := jail.New("systemd-run")
			Expect(err).NotTo(HaveOccurred())
			Expect(j).NotTo(BeNil())
		})

		It("returns an error for an unknown backend", func() {
			j, err := jail.New("docker")
			Expect(err).To(MatchError(`unknown jail backend: "docker"`))
			Expect(j).To(BeNil())
		})
	})

	Describe("NewWithRunner", func() {
		It("injects a custom runner", func() {
			var c captured
			j, err := jail.NewWithRunner("systemd-run", recordingRunner(&c))
			Expect(err).NotTo(HaveOccurred())
			Expect(j).NotTo(BeNil())
		})

		It("returns an error for an unknown backend", func() {
			j, err := jail.NewWithRunner("podman", nil)
			Expect(err).To(MatchError(`unknown jail backend: "podman"`))
			Expect(j).To(BeNil())
		})
	})

	Describe("SystemdJail.Run", func() {
		var (
			c    captured
			j    jail.Jail
			opts jail.RunOpts
			home string
		)

		BeforeEach(func() {
			c = captured{}
			var err error
			j, err = jail.NewWithRunner("systemd-run", recordingRunner(&c))
			Expect(err).NotTo(HaveOccurred())

			home = os.Getenv("HOME")

			opts = jail.RunOpts{
				Workspace: &workspace.Workspace{
					Path:     "/tmp/hive/ws-123",
					RepoPath: "/home/ivy/src/myrepo",
				},
				Command: []string{"claude", "-p", "--model", "sonnet"},
				Env:     []string{"FOO=bar", "BAZ=qux"},
				APIKey:  "sk-ant-test-key",
			}
		})

		It("invokes systemd-run", func() {
			err := j.Run(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.name).To(Equal("systemd-run"))
		})

		It("passes --user and --pipe flags", func() {
			err := j.Run(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.args).To(ContainElements("--user", "--pipe"))
		})

		It("sets TemporaryFileSystem on $HOME", func() {
			err := j.Run(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.args).To(ContainElement("TemporaryFileSystem=" + home + ":mode=0755"))
		})

		It("enables ProtectSystem=strict", func() {
			err := j.Run(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.args).To(ContainElement("ProtectSystem=strict"))
		})

		It("enables PrivateTmp=yes", func() {
			err := j.Run(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.args).To(ContainElement("PrivateTmp=yes"))
		})

		It("enables NoNewPrivileges=yes", func() {
			err := j.Run(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.args).To(ContainElement("NoNewPrivileges=yes"))
		})

		Context("BindPaths", func() {
			It("bind-mounts the worktree", func() {
				err := j.Run(context.Background(), opts)
				Expect(err).NotTo(HaveOccurred())

				bindArgs := flagValues(c.args, "BindPaths=")
				Expect(bindArgs).To(ContainElement("/tmp/hive/ws-123"))
			})

			It("bind-mounts the repo .git directory", func() {
				err := j.Run(context.Background(), opts)
				Expect(err).NotTo(HaveOccurred())

				bindArgs := flagValues(c.args, "BindPaths=")
				Expect(bindArgs).To(ContainElement("/home/ivy/src/myrepo/.git"))
			})

			It("bind-mounts ~/.claude", func() {
				err := j.Run(context.Background(), opts)
				Expect(err).NotTo(HaveOccurred())

				bindArgs := flagValues(c.args, "BindPaths=")
				Expect(bindArgs).To(ContainElement(home + "/.claude"))
			})
		})

		Context("BindReadOnlyPaths", func() {
			It("read-only mounts ~/.local/bin", func() {
				err := j.Run(context.Background(), opts)
				Expect(err).NotTo(HaveOccurred())

				roArgs := flagValues(c.args, "BindReadOnlyPaths=")
				Expect(roArgs).To(ContainElement(home + "/.local/bin"))
			})

			It("read-only mounts ~/.local/share/mise", func() {
				err := j.Run(context.Background(), opts)
				Expect(err).NotTo(HaveOccurred())

				roArgs := flagValues(c.args, "BindReadOnlyPaths=")
				Expect(roArgs).To(ContainElement(home + "/.local/share/mise"))
			})

			It("read-only mounts ~/.local/share/claude", func() {
				err := j.Run(context.Background(), opts)
				Expect(err).NotTo(HaveOccurred())

				roArgs := flagValues(c.args, "BindReadOnlyPaths=")
				Expect(roArgs).To(ContainElement(home + "/.local/share/claude"))
			})
		})

		It("ends args with -- /bin/bash -c <bootstrap>", func() {
			err := j.Run(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())

			// Find the "--" separator
			dashIdx := -1
			for i, a := range c.args {
				if a == "--" {
					dashIdx = i
					break
				}
			}
			Expect(dashIdx).To(BeNumerically(">", 0))
			Expect(c.args[dashIdx+1]).To(Equal("/bin/bash"))
			Expect(c.args[dashIdx+2]).To(Equal("-c"))
			// Bootstrap script is the last arg
			Expect(len(c.args)).To(Equal(dashIdx + 4))
		})

		Context("bootstrap script", func() {
			var bootstrap string

			BeforeEach(func() {
				err := j.Run(context.Background(), opts)
				Expect(err).NotTo(HaveOccurred())
				// Bootstrap is the last argument
				bootstrap = c.args[len(c.args)-1]
			})

			It("exports PATH with mise shims", func() {
				Expect(bootstrap).To(ContainSubstring(
					`export PATH="` + home + `/.local/bin:` + home + `/.local/share/mise/shims:$PATH"`,
				))
			})

			It("sets git identity", func() {
				Expect(bootstrap).To(ContainSubstring(`git config --global user.name "Claude (hive)"`))
				Expect(bootstrap).To(ContainSubstring(`git config --global user.email "claude-hive@localhost"`))
			})

			It("exports MISE_YES=1", func() {
				Expect(bootstrap).To(ContainSubstring("export MISE_YES=1"))
			})

			It("exports MISE_TRUSTED_CONFIG_PATHS for the worktree", func() {
				Expect(bootstrap).To(ContainSubstring(`MISE_TRUSTED_CONFIG_PATHS="/tmp/hive/ws-123"`))
			})

			It("exports the API key when set", func() {
				Expect(bootstrap).To(ContainSubstring(`ANTHROPIC_API_KEY="sk-ant-test-key"`))
			})

			It("exports additional environment variables", func() {
				Expect(bootstrap).To(ContainSubstring(`export FOO="bar"`))
				Expect(bootstrap).To(ContainSubstring(`export BAZ="qux"`))
			})

			It("changes to the worktree directory", func() {
				Expect(bootstrap).To(ContainSubstring(`cd "/tmp/hive/ws-123"`))
			})

			It("execs the command", func() {
				Expect(bootstrap).To(ContainSubstring(`exec 'claude' '-p' '--model' 'sonnet'`))
			})
		})

		Context("when APIKey is empty (subscription auth)", func() {
			BeforeEach(func() {
				opts.APIKey = ""
				err := j.Run(context.Background(), opts)
				Expect(err).NotTo(HaveOccurred())
			})

			It("does not export ANTHROPIC_API_KEY", func() {
				bootstrap := c.args[len(c.args)-1]
				Expect(bootstrap).NotTo(ContainSubstring("ANTHROPIC_API_KEY"))
			})
		})
	})

	Describe("SystemdJail.RunCapture", func() {
		var (
			c    captured
			j    jail.Jail
			opts jail.RunOpts
		)

		BeforeEach(func() {
			c = captured{}
			var err error
			j, err = jail.NewWithRunner("systemd-run", recordingRunner(&c))
			Expect(err).NotTo(HaveOccurred())

			opts = jail.RunOpts{
				Workspace: &workspace.Workspace{
					Path:     "/tmp/hive/ws-456",
					RepoPath: "/home/ivy/src/myrepo",
				},
				Command: []string{"claude", "-p", "--output-format", "json"},
			}
		})

		It("invokes systemd-run", func() {
			_, err := j.RunCapture(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.name).To(Equal("systemd-run"))
		})

		It("uses --pipe instead of --pty", func() {
			_, err := j.RunCapture(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.args).To(ContainElement("--pipe"))
			Expect(c.args).NotTo(ContainElement("--pty"))
		})

		It("passes --user flag", func() {
			_, err := j.RunCapture(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.args).To(ContainElement("--user"))
		})

		It("sets the same sandbox properties as Run", func() {
			_, err := j.RunCapture(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.args).To(ContainElement("ProtectSystem=strict"))
			Expect(c.args).To(ContainElement("PrivateTmp=yes"))
			Expect(c.args).To(ContainElement("NoNewPrivileges=yes"))
		})

		It("bind-mounts the worktree", func() {
			_, err := j.RunCapture(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())

			bindArgs := flagValues(c.args, "BindPaths=")
			Expect(bindArgs).To(ContainElement("/tmp/hive/ws-456"))
		})

		It("ends args with -- /bin/bash -c <bootstrap>", func() {
			_, err := j.RunCapture(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())

			dashIdx := -1
			for i, a := range c.args {
				if a == "--" {
					dashIdx = i
					break
				}
			}
			Expect(dashIdx).To(BeNumerically(">", 0))
			Expect(c.args[dashIdx+1]).To(Equal("/bin/bash"))
			Expect(c.args[dashIdx+2]).To(Equal("-c"))
		})
	})
})

// flagValues extracts the values from -p args that start with the given prefix.
// For example, flagValues(args, "BindPaths=") returns all BindPaths values.
func flagValues(args []string, prefix string) []string {
	var vals []string
	for i, a := range args {
		if a == "-p" && i+1 < len(args) {
			v := args[i+1]
			if after, ok := strings.CutPrefix(v, prefix); ok {
				vals = append(vals, after)
			}
		}
	}
	return vals
}
