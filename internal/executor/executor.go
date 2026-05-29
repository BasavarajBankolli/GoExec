package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/BasavarajBankolli/goexec/api"
)

var languageImage = map[api.Language]string{
	api.LangGo:     "goexec-sandbox-go:latest",
	api.LangPython: "goexec-sandbox-python:latest",
	api.LangCpp:    "goexec-sandbox-cpp:latest",
	api.LangJava:   "goexec-sandbox-java:latest",
}

var languageFile = map[api.Language]string{
	api.LangGo:     "main.go",
	api.LangPython: "main.py",
	api.LangCpp:    "main.cpp",
	api.LangJava:   "Main.java",
}

// languageRunCmd uses go build+exec instead of go run to avoid recompiling
// the toolchain on every invocation. With the pre-warmed GOCACHE in the
// sandbox image, go build completes in <500ms.
var languageRunCmd = map[api.Language]string{
	api.LangGo:     "cd /sandbox && go build -o /tmp/goexec_prog main.go && /tmp/goexec_prog",
	api.LangPython: "cd /sandbox && python3 main.py",
	api.LangCpp:    "cd /sandbox && g++ -O2 -o /tmp/goexec_prog main.cpp && /tmp/goexec_prog",
	api.LangJava:   "cd /sandbox && javac Main.java && java -cp /sandbox Main",
}

// languageEnv adds runtime-specific environment variables per language.
var languageEnv = map[api.Language][]string{
	// GOCACHE points to the pre-warmed cache baked into the sandbox image.
	api.LangGo: {"GOCACHE=/goexec-cache"},
}

// Per-language pids limits. Go compiler spawns many OS threads (GC, parallel
// compile workers) and needs at least 256.
var languagePidsLimit = map[api.Language]int64{
	api.LangGo:     512,
	api.LangPython: 256,
	api.LangCpp:    256,
	api.LangJava:   512,
}

type Executor struct {
	cli *client.Client
}

func New() (*Executor, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Executor{cli: cli}, nil
}

func (e *Executor) RunJob(job api.Job) api.Result {
	req := job.Request
	start := time.Now()

	image, ok := languageImage[req.Language]
	if !ok {
		return api.Result{
			JobID:   job.ID,
			Verdict: api.VerdictSystemError,
			Stderr:  fmt.Sprintf("unsupported language: %s", req.Language),
		}
	}

	const (
		maxCPU   = 2.0
		maxMemMB = 512
	)

	cpuQuota := clampFloat(req.CPUQuota, 0.1, maxCPU)
	memMB    := clampInt64(req.MemoryMB, 128, maxMemMB) // min 128 MB — Go runtime needs it
	timeout  := clampDuration(
		time.Duration(req.TimeoutMs)*time.Millisecond,
		1*time.Second,
		30*time.Second, // max 30s — allows compilation + execution
	)

	cpuQuotaMicros := int64(cpuQuota * 100_000)
	pidsLimit      := languagePidsLimit[req.Language]
	filename       := languageFile[req.Language]
	runCmd         := languageRunCmd[req.Language]

	// Build env: base vars + language-specific vars.
	env := []string{
		"GOEXEC_CODE=" + req.Code,
		"GOEXEC_STDIN=" + req.Stdin,
	}
	env = append(env, languageEnv[req.Language]...)

	entrypoint := fmt.Sprintf(
		`printf '%%s' "$GOEXEC_CODE" > /sandbox/%s && %s`,
		filename, runCmd,
	)

	cfg := &container.Config{
		Image:        image,
		Env:          env,
		Entrypoint:   []string{"sh", "-c", entrypoint},
		AttachStdout: true,
		AttachStderr: true,
	}

	hostCfg := &container.HostConfig{
		Resources: container.Resources{
			CPUPeriod:  100_000,
			CPUQuota:   cpuQuotaMicros,
			Memory:     memMB * 1024 * 1024,
			MemorySwap: memMB * 1024 * 1024,
			PidsLimit:  &pidsLimit,
		},
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges"},
		NetworkMode: "none",
		AutoRemove:  false,
	}

	ctx := context.Background()

	ctr, err := e.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return api.Result{JobID: job.ID, Verdict: api.VerdictSystemError, Stderr: err.Error()}
	}
	defer e.cli.ContainerRemove(ctx, ctr.ID, types.ContainerRemoveOptions{Force: true}) //nolint:errcheck

	if err := e.cli.ContainerStart(ctx, ctr.ID, types.ContainerStartOptions{}); err != nil {
		return api.Result{JobID: job.ID, Verdict: api.VerdictSystemError, Stderr: err.Error()}
	}

	execCtx, execCancel := context.WithTimeout(ctx, timeout)
	defer execCancel()

	statusCh, errCh := e.cli.ContainerWait(execCtx, ctr.ID, container.WaitConditionNotRunning)

	var exitCode int
	var tle bool

	select {
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
	case <-errCh:
		tle = true
		e.cli.ContainerKill(ctx, ctr.ID, "SIGKILL") //nolint:errcheck
	case <-execCtx.Done():
		tle = true
		e.cli.ContainerKill(ctx, ctr.ID, "SIGKILL") //nolint:errcheck
	}

	elapsed := time.Since(start)

	logReader, err := e.cli.ContainerLogs(ctx, ctr.ID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	var stdout, stderr bytes.Buffer
	if err == nil {
		readDockerLogs(logReader, &stdout, &stderr)
		logReader.Close()
	}

	verdict := classify(exitCode, tle, stdout.String(), stderr.String())

	return api.Result{
		JobID:    job.ID,
		Verdict:  verdict,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: elapsed,
	}
}

func classify(code int, tle bool, stdout, stderr string) api.Verdict {
	if tle {
		return api.VerdictTimeLimitExceeded
	}
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "out of memory") || strings.Contains(lower, "killed") {
		return api.VerdictMemoryLimitExceeded
	}
	if code == 0 {
		return api.VerdictAccepted
	}
	if isCompileError(lower) {
		return api.VerdictCompileError
	}
	return api.VerdictRuntimeError
}

func readDockerLogs(r io.Reader, stdout, stderr *bytes.Buffer) {
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			break
		}
		size := int(hdr[4])<<24 | int(hdr[5])<<16 | int(hdr[6])<<8 | int(hdr[7])
		frame := make([]byte, size)
		if _, err := io.ReadFull(r, frame); err != nil {
			break
		}
		switch hdr[0] {
		case 1:
			stdout.Write(frame)
		case 2:
			stderr.Write(frame)
		}
	}
}

func clampFloat(v, lo, hi float64) float64 {
	if v <= 0 { return (lo + hi) / 2 }
	if v < lo  { return lo }
	if v > hi  { return hi }
	return v
}

func clampInt64(v, lo, hi int64) int64 {
	if v <= 0 { return (lo + hi) / 2 }
	if v < lo  { return lo }
	if v > hi  { return hi }
	return v
}

func clampDuration(v, lo, hi time.Duration) time.Duration {
	if v <= 0 { return (lo + hi) / 2 }
	if v < lo  { return lo }
	if v > hi  { return hi }
	return v
}

// isCompileError distinguishes compiler/parser failures from runtime exceptions.
//
// The naive check strings.Contains(stderr, "error:") is too broad — Python
// runtime exceptions like "ZeroDivisionError:" and "NameError:" also contain
// "error:", causing them to be wrongly labelled Compile Error.
//
// We match compiler-specific output patterns per language:
//   - Python : only SyntaxError / IndentationError are parse-time failures
//   - C++    : gcc format is  "file.cpp:line:col: error: …"
//   - Go     : format is      "./main.go:line:col: …"
//   - Java   : javac format is "file.java:line: error: …"
func isCompileError(lower string) bool {
	// Python compile-time errors only — ZeroDivisionError etc. are runtime.
	if strings.Contains(lower, "syntaxerror:") ||
		strings.Contains(lower, "indentationerror:") ||
		strings.Contains(lower, "taberror:") {
		return true
	}
	// C++ gcc: "main.cpp:5:3: error:"
	if strings.Contains(lower, ".cpp:") && strings.Contains(lower, ": error:") {
		return true
	}
	// Go compiler: "./main.go:4:2: undefined" etc.
	if strings.Contains(lower, ".go:") && strings.Contains(lower, ": ") {
		return true
	}
	// Java javac: "Main.java:5: error:"
	if strings.Contains(lower, ".java:") && strings.Contains(lower, ": error:") {
		return true
	}
	// Generic fallback
	if strings.Contains(lower, "compilationerror") {
		return true
	}
	return false
}
