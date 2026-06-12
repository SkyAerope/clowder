// Package script 执行 provider 外挂脚本（register / check）。
// 协议：stdout 最后一行 JSON；退出码 0 = 成功；超时 kill 整个进程树。
// 同一 provider 的 register 串行执行；失败按 provider 维度指数退避。
package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// RegisterResult 是 register.py 的 stdout JSON。
type RegisterResult struct {
	APIKey   string         `json:"api_key"`
	Metadata map[string]any `json:"metadata"`
}

// CheckResult 是 check.py 的 stdout JSON。
type CheckResult struct {
	Status   string         `json:"status"` // active|invalid|rate_limited|error
	Metadata map[string]any `json:"metadata"`
}

// Runner 管理脚本执行的串行锁与退避状态。
type Runner struct {
	mu sync.Mutex
	// per-provider register 串行锁
	regLocks map[string]*sync.Mutex
	// per-provider 退避状态
	backoff map[string]*backoffState

	BackoffBase time.Duration // 注册失败退避基数
	BackoffMax  time.Duration // 0 = 不设上限
}

type backoffState struct {
	failures int
	until    time.Time
}

// NewRunner 创建 Runner。
func NewRunner(base, max time.Duration) *Runner {
	return &Runner{
		regLocks:    map[string]*sync.Mutex{},
		backoff:     map[string]*backoffState{},
		BackoffBase: base,
		BackoffMax:  max,
	}
}

func (r *Runner) regLock(provider string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.regLocks[provider]
	if !ok {
		l = &sync.Mutex{}
		r.regLocks[provider] = l
	}
	return l
}

// InBackoff 返回该 provider 自动注册是否处于退避期（手动注册不调用此方法）。
func (r *Runner) InBackoff(provider string) (bool, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.backoff[provider]
	if !ok || time.Now().After(st.until) {
		return false, time.Time{}
	}
	return true, st.until
}

func (r *Runner) recordFailure(provider string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.backoff[provider]
	if !ok {
		st = &backoffState{}
		r.backoff[provider] = st
	}
	d := r.BackoffBase * (1 << min(st.failures, 30))
	if r.BackoffMax > 0 && d > r.BackoffMax {
		d = r.BackoffMax
	}
	st.failures++
	st.until = time.Now().Add(d)
}

func (r *Runner) recordSuccess(provider string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.backoff, provider)
}

// ResetBackoff 手动注册时重置退避计时（设计：手动 register 会重置退避）。
func (r *Runner) ResetBackoff(provider string) {
	r.recordSuccess(provider)
}

// Register 执行注册脚本（同 provider 串行）。manual=true 跳过退避检查并重置退避。
func (r *Runner) Register(ctx context.Context, provider, dir, command string, timeout time.Duration, manual bool) (*RegisterResult, error) {
	if !manual {
		if in, until := r.InBackoff(provider); in {
			return nil, fmt.Errorf("provider %s register in backoff until %s", provider, until.Format(time.RFC3339))
		}
	} else {
		r.ResetBackoff(provider)
	}
	l := r.regLock(provider)
	l.Lock()
	defer l.Unlock()

	out, err := runScript(ctx, dir, command, nil, timeout)
	if err != nil {
		r.recordFailure(provider)
		return nil, err
	}
	res := &RegisterResult{}
	if err := decodeLastJSON(out, res); err != nil {
		r.recordFailure(provider)
		return nil, fmt.Errorf("register output invalid: %w (stdout: %s)", err, truncate(out, 500))
	}
	if res.APIKey == "" {
		r.recordFailure(provider)
		return nil, fmt.Errorf("register output missing api_key (stdout: %s)", truncate(out, 500))
	}
	r.recordSuccess(provider)
	return res, nil
}

// Check 执行 check 脚本，argv[1] = api_key。
func (r *Runner) Check(ctx context.Context, dir, command, apiKey string, timeout time.Duration) (*CheckResult, error) {
	out, err := runScript(ctx, dir, command, []string{apiKey}, timeout)
	if err != nil {
		return nil, err
	}
	res := &CheckResult{}
	if err := decodeLastJSON(out, res); err != nil {
		return nil, fmt.Errorf("check output invalid: %w (stdout: %s)", err, truncate(out, 500))
	}
	switch res.Status {
	case "active", "invalid", "rate_limited", "error":
	default:
		return nil, fmt.Errorf("check output invalid status %q", res.Status)
	}
	return res, nil
}

// runScript 在 dir 下执行 shell 命令，超时 kill 进程树（负 pgid）。
func runScript(ctx context.Context, dir, command string, extraArgs []string, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	full := command
	for _, a := range extraArgs {
		full += " " + shellQuote(a)
	}
	cmd := exec.Command("sh", "-c", full)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // 独立进程组，便于整树 kill
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start script: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-cctx.Done():
		// 杀整个进程组
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return "", fmt.Errorf("script timed out after %s", timeout)
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("script failed: %w (stderr: %s)", err, truncate(stderr.String(), 500))
		}
		return stdout.String(), nil
	}
}

// decodeLastJSON 取 stdout 最后一个非空行解析 JSON（容忍脚本前面打印日志）。
func decodeLastJSON(out string, v any) error {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), v); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no valid JSON line in output")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
