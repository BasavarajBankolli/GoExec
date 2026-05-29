package api

import "time"

type Language string

const (
	LangGo     Language = "go"
	LangPython Language = "python"
	LangCpp    Language = "cpp"
	LangJava   Language = "java"
)

type Verdict string

const (
	VerdictAccepted            Verdict = "Accepted"
	VerdictTimeLimitExceeded   Verdict = "Time Limit Exceeded"
	VerdictMemoryLimitExceeded Verdict = "Memory Limit Exceeded"
	VerdictRuntimeError        Verdict = "Runtime Error"
	VerdictCompileError        Verdict = "Compile Error"
	VerdictSystemError         Verdict = "System Error"
)

type SubmitRequest struct {
	Code      string   `json:"code"`
	Language  Language `json:"language"`
	Stdin     string   `json:"stdin,omitempty"`
	CPUQuota  float64  `json:"cpu_quota,omitempty"`
	MemoryMB  int64    `json:"memory_mb,omitempty"`
	TimeoutMs int64    `json:"timeout_ms,omitempty"`
}

type Job struct {
	ID       string
	Request  SubmitRequest
	ResultCh chan Result
}

type Result struct {
	JobID     string        `json:"job_id"`
	Verdict   Verdict       `json:"verdict"`
	Stdout    string        `json:"stdout"`
	Stderr    string        `json:"stderr"`
	ExitCode  int           `json:"exit_code"`
	Duration  time.Duration `json:"duration_ms"`
	MemUsedMB float64       `json:"mem_used_mb"`
	Cached    bool          `json:"cached"`
}

type SubmitResponse struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

type WSEvent struct {
	Type    string  `json:"type"`
	Payload string  `json:"payload,omitempty"`
	Result  *Result `json:"result,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
