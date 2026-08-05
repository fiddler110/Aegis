package sandbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCLI stands in for the container runtime binary, recording every
// invocation and replaying scripted results. It is what makes the P60.2
// lifecycle (start once, reuse, restart a vanished container, tear down)
// testable on a machine with no container runtime installed.
type fakeCLI struct {
	mu    sync.Mutex
	calls [][]string

	// runFn, when set, decides the result of each run call by argument list.
	runFn func(args []string) (string, error)
}

func (f *fakeCLI) record(args []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), args...))
}

func (f *fakeCLI) run(_ context.Context, args []string) (string, error) {
	f.record(args)
	if f.runFn != nil {
		return f.runFn(args)
	}
	if len(args) > 0 && args[0] == "run" {
		return "deadbeefcafe0000\n", nil
	}
	return "", nil
}

func (f *fakeCLI) stream(_ context.Context, args []string, emit func(string)) error {
	f.record(args)
	var err error
	if f.runFn != nil {
		var out string
		out, err = f.runFn(args)
		if out != "" && emit != nil {
			emit(out)
		}
	}
	return err
}

func (f *fakeCLI) verbs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if len(c) > 0 {
			out = append(out, c[0])
		}
	}
	return out
}

func (f *fakeCLI) countVerb(verb string) int {
	n := 0
	for _, v := range f.verbs() {
		if v == verb {
			n++
		}
	}
	return n
}

func newPersistentBackend(cli containerCLI) *ContainerBackend {
	return &ContainerBackend{
		runtime:    RuntimeDocker,
		image:      "ubuntu:22.04",
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		persistent: true,
		containers: map[string]string{},
		cli:        cli,
	}
}

// TestPersistentStartsOnceAndExecsAfter is the P60.2 property in one test: the
// second command must not start a second container, because that is precisely
// what discarded everything the first one did.
func TestPersistentStartsOnceAndExecsAfter(t *testing.T) {
	cli := &fakeCLI{}
	c := newPersistentBackend(cli)

	for i := 0; i < 3; i++ {
		if _, err := c.Exec(context.Background(), "echo hi", ExecOpts{Dir: "/work"}); err != nil {
			t.Fatalf("Exec %d: %v", i, err)
		}
	}
	if got := cli.countVerb("run"); got != 1 {
		t.Errorf("container starts = %d, want 1 for three commands in one directory", got)
	}
	if got := cli.countVerb("exec"); got != 3 {
		t.Errorf("exec calls = %d, want 3", got)
	}
}

// TestPersistentExecTargetsTheStartedContainer: the exec must name the id the
// start returned, in the workspace working directory.
func TestPersistentExecTargetsTheStartedContainer(t *testing.T) {
	cli := &fakeCLI{runFn: func(args []string) (string, error) {
		if args[0] == "run" {
			return "abc123def456\n", nil
		}
		return "", nil
	}}
	c := newPersistentBackend(cli)
	if _, err := c.Exec(context.Background(), "go test ./...", ExecOpts{Dir: "/work"}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	last := cli.calls[len(cli.calls)-1]
	joined := strings.Join(last, " ")
	if last[0] != "exec" {
		t.Fatalf("second call verb = %q, want exec", last[0])
	}
	if !strings.Contains(joined, "abc123def456") {
		t.Errorf("exec does not target the started container: %v", last)
	}
	if !strings.Contains(joined, "-w /workspace") {
		t.Errorf("exec does not run in the workspace: %v", last)
	}
	if !strings.Contains(joined, "go test ./...") {
		t.Errorf("exec lost the command: %v", last)
	}
}

// TestPersistentStartKeepsHardeningAndLimits: persistence must not become a
// quiet way to get a weaker container. The detached run carries the same
// hardening flags, resource caps and network posture the per-command run does.
func TestPersistentStartKeepsHardeningAndLimits(t *testing.T) {
	c := newPersistentBackend(&fakeCLI{})
	c.limits = ResourceLimits{Memory: "4G", CPUs: "2", PIDs: 512}
	args := strings.Join(c.startPersistentArgs("/work"), " ")

	for _, want := range []string{
		"run -d --rm",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--memory 4G",
		"--cpus 2",
		"--pids-limit 512",
		"--network none",
		"-v /work:/workspace -w /workspace",
		"--label " + persistentLabel + "=1",
		"--label " + persistentOwnerLabel + "=",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("start args missing %q:\n%s", want, args)
		}
	}
}

// TestPersistentStartIsTTLBounded: the container holds itself open with a
// bounded sleep under --rm, so a daemon that dies without calling Close leaves
// something that removes itself rather than a permanent resident.
func TestPersistentStartIsTTLBounded(t *testing.T) {
	c := newPersistentBackend(&fakeCLI{})
	c.sessionTTL = 90 * time.Second
	args := c.startPersistentArgs("/work")
	last := args[len(args)-1]
	if last != "sleep 90" {
		t.Errorf("container entrypoint = %q, want a bounded sleep", last)
	}

	c.sessionTTL = 0
	args = c.startPersistentArgs("/work")
	if got, want := args[len(args)-1], "sleep 14400"; got != want {
		t.Errorf("default TTL entrypoint = %q, want %q (DefaultSessionTTL)", got, want)
	}
}

// TestPersistentRestartsAVanishedContainer: a TTL expiry or an operator's
// `docker rm` must read as a slow command, not a failed one.
func TestPersistentRestartsAVanishedContainer(t *testing.T) {
	execs := 0
	cli := &fakeCLI{}
	cli.runFn = func(args []string) (string, error) {
		switch args[0] {
		case "run":
			return "newcontainerid\n", nil
		case "exec":
			execs++
			if execs == 1 {
				return "Error response from daemon: No such container: old", errors.New("exit status 1")
			}
			return "ok", nil
		}
		return "", nil
	}
	c := newPersistentBackend(cli)
	c.containers["/work"] = "old"

	out, err := c.Exec(context.Background(), "ls", ExecOpts{Dir: "/work"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if out != "ok" {
		t.Errorf("output = %q, want the retried command's output", out)
	}
	if got := cli.countVerb("run"); got != 1 {
		t.Errorf("container starts = %d, want 1 (a replacement for the vanished one)", got)
	}
	if execs != 2 {
		t.Errorf("exec attempts = %d, want 2 (original + one retry)", execs)
	}
}

// TestPersistentRetriesOnlyOnce: a container that keeps vanishing is a real
// error, reported as one rather than retried forever.
func TestPersistentRetriesOnlyOnce(t *testing.T) {
	cli := &fakeCLI{runFn: func(args []string) (string, error) {
		if args[0] == "run" {
			return "id\n", nil
		}
		return "no such container", errors.New("exit status 1")
	}}
	c := newPersistentBackend(cli)
	if _, err := c.Exec(context.Background(), "ls", ExecOpts{Dir: "/work"}); err == nil {
		t.Fatal("expected an error when the container keeps vanishing")
	}
	if got := cli.countVerb("exec"); got != 2 {
		t.Errorf("exec attempts = %d, want exactly 2", got)
	}
}

// TestPersistentFailedStartDegradesToOneShot: if the container cannot start,
// commands must still run — one-shot, as before P60.2 — and the backend must
// stop retrying the start on every subsequent call.
func TestPersistentFailedStartDegradesToOneShot(t *testing.T) {
	cli := &fakeCLI{runFn: func(args []string) (string, error) {
		if len(args) > 1 && args[1] == "-d" {
			return "cannot start", errors.New("exit status 125")
		}
		return "output", nil
	}}
	c := newPersistentBackend(cli)

	for i := 0; i < 3; i++ {
		if _, err := c.Exec(context.Background(), "echo hi", ExecOpts{Dir: "/work"}); err != nil {
			t.Fatalf("Exec %d: %v", i, err)
		}
	}
	starts := 0
	for _, call := range cli.calls {
		if len(call) > 1 && call[0] == "run" && call[1] == "-d" {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("detached start attempts = %d, want 1 (degrade once, then stop trying)", starts)
	}
	if got := cli.countVerb("exec"); got != 0 {
		t.Errorf("exec calls = %d, want 0 after degrading to one-shot runs", got)
	}
	if c.persistent {
		t.Error("backend still reports persistent after a failed start")
	}
}

// TestPersistentPerDirectoryAndCapped: a second workspace root gets its own
// container (its mount differs), and the count is capped so an unusual set of
// directories cannot leak containers.
func TestPersistentPerDirectoryAndCapped(t *testing.T) {
	cli := &fakeCLI{}
	c := newPersistentBackend(cli)

	dirs := []string{"/a", "/b", "/c", "/d", "/e", "/f"}
	for _, d := range dirs {
		if _, err := c.Exec(context.Background(), "ls", ExecOpts{Dir: d}); err != nil {
			t.Fatalf("Exec %s: %v", d, err)
		}
	}
	starts := 0
	for _, call := range cli.calls {
		if len(call) > 1 && call[0] == "run" && call[1] == "-d" {
			starts++
		}
	}
	if starts != maxPersistentContainers {
		t.Errorf("containers started = %d, want the cap %d", starts, maxPersistentContainers)
	}
	// The directories past the cap still ran — one-shot, not refused.
	if got := len(cli.calls); got != len(dirs)+maxPersistentContainers {
		t.Errorf("total calls = %d, want %d (a start per capped dir + a command per dir)", got, len(dirs)+maxPersistentContainers)
	}
}

// TestPersistentCloseRemovesContainers: Close() is where owned state is given
// back. Without it, `--rm`'s leak-freedom is traded away for nothing.
func TestPersistentCloseRemovesContainers(t *testing.T) {
	cli := &fakeCLI{}
	c := newPersistentBackend(cli)
	for _, d := range []string{"/a", "/b"} {
		if _, err := c.Exec(context.Background(), "ls", ExecOpts{Dir: d}); err != nil {
			t.Fatalf("Exec: %v", err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	removals := 0
	for _, call := range cli.calls {
		if len(call) >= 2 && call[0] == "rm" && call[1] == "-f" {
			removals++
		}
	}
	if removals != 2 {
		t.Errorf("rm -f calls = %d, want 2 (one per started container)", removals)
	}
	if len(c.containers) != 0 {
		t.Errorf("containers still tracked after Close: %v", c.containers)
	}
}

// TestNonPersistentUnchanged: with persistence off, every command is still its
// own `run --rm` and Close still has nothing to do — the pre-P60.2 behavior an
// operator can ask for explicitly.
func TestNonPersistentUnchanged(t *testing.T) {
	cli := &fakeCLI{}
	c := newPersistentBackend(cli)
	c.persistent = false

	for i := 0; i < 2; i++ {
		if _, err := c.Exec(context.Background(), "ls", ExecOpts{Dir: "/work"}); err != nil {
			t.Fatalf("Exec: %v", err)
		}
	}
	if got := cli.countVerb("run"); got != 2 {
		t.Errorf("run calls = %d, want one per command", got)
	}
	if got := cli.countVerb("exec"); got != 0 {
		t.Errorf("exec calls = %d, want 0", got)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := cli.countVerb("rm"); got != 0 {
		t.Errorf("rm calls = %d, want 0 with nothing owned", got)
	}
}

// TestSupportsPersistentContainerOnlyWhereVerified: the same "only where the
// CLI surface is verified" rule OCIHardeningFlags and ResourceFlags follow —
// applying `run -d`/`exec` to a runtime that does not have them is not a
// degraded feature, it is a failed command.
func TestSupportsPersistentContainerOnlyWhereVerified(t *testing.T) {
	for rt, want := range map[ContainerRuntime]bool{
		RuntimeDocker:          true,
		RuntimePodman:          true,
		RuntimeWSL:             false,
		RuntimeAppleContainers: false,
	} {
		if got := SupportsPersistentContainer(rt); got != want {
			t.Errorf("SupportsPersistentContainer(%q) = %v, want %v", rt, got, want)
		}
	}
}

// TestContainerGoneDistinguishesFailureKinds: a command that exits non-zero
// inside a healthy container is a normal result the model must see, not a
// reason to restart the sandbox underneath it.
func TestContainerGoneDistinguishesFailureKinds(t *testing.T) {
	if !containerGone("Error: No such container: abc", errors.New("exit status 1")) {
		t.Error("a missing container was not recognized")
	}
	if !containerGone("Error response from daemon: Container abc is not running", errors.New("exit status 1")) {
		t.Error("a stopped container was not recognized")
	}
	if containerGone("test.go:12: assertion failed", errors.New("exit status 1")) {
		t.Error("an ordinary failing command was mistaken for a missing container")
	}
	if containerGone("", nil) {
		t.Error("a successful command was mistaken for a missing container")
	}
}

// TestProcessAliveKnowsItself guards the reaper's safety property: it must
// never mistake a live Aegis process for a dead one, or it would tear the
// sandbox out from under a working session.
func TestProcessAliveKnowsItself(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("processAlive reported this very process as dead")
	}
	if processAlive(0) || processAlive(-1) {
		t.Error("processAlive accepted a non-pid")
	}
}
