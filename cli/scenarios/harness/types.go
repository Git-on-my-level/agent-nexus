package harness

import "time"

type Mode string

const (
	ModeDeterministic Mode = "deterministic"
	ModeLLM           Mode = "llm"

	DefaultOpenAICompatBaseURL = "https://api.z.ai/api/coding/paas/v4"
	DefaultOpenAICompatModel   = "glm-4.7-flashx"
)

type Scenario struct {
	Name       string      `json:"name"`
	BaseURL    string      `json:"base_url"`
	Agents     []AgentSpec `json:"agents"`
	Assertions []Assertion `json:"assertions"`
}

type AgentSpec struct {
	Name               string  `json:"name"`
	UsernamePrefix     string  `json:"username_prefix"`
	DeterministicSteps []Step  `json:"deterministic_steps"`
	LLM                LLMSpec `json:"llm"`
}

type LLMSpec struct {
	Objective         string `json:"objective"`
	ProfilePath       string `json:"profile_path"`
	MaxTurns          int    `json:"max_turns"`
	MinSuccessfulRuns int    `json:"min_successful_runs,omitempty"`
}

type Step struct {
	Name         string         `json:"name"`
	Args         []string       `json:"args"`
	Stdin        map[string]any `json:"stdin"`
	Capture      map[string]any `json:"capture"`
	AllowFailure bool           `json:"allow_failure"`
	ExpectError  *ExpectedError `json:"expect_error,omitempty"`
}

type ExpectedError struct {
	ExitCode        *int   `json:"exit_code,omitempty"`
	Status          *int   `json:"status,omitempty"`
	Code            string `json:"code,omitempty"`
	MessageContains string `json:"message_contains,omitempty"`
}

type Assertion struct {
	Name      string         `json:"name"`
	Agent     string         `json:"agent"`
	Args      []string       `json:"args"`
	Stdin     map[string]any `json:"stdin"`
	Contains  []string       `json:"contains"`
	JSONPaths map[string]any `json:"json_paths"`
}

type Config struct {
	ScenarioPath      string
	OARBinary         string
	BaseURLOverride   string
	Mode              Mode
	LLMDriverBin      string
	LLMDriverArgs     []string
	LLMAPIBase        string
	LLMAPIKey         string
	LLMModel          string
	LLMTemperature    float64
	LLMMaxTokens      int
	LLMTimeoutSeconds int
	LLMRetryAttempts  int
	Verbose           bool
	WorkingDirectory  string
}

type Report struct {
	Scenario      string                    `json:"scenario"`
	Mode          Mode                      `json:"mode"`
	RunID         string                    `json:"run_id"`
	StartedAt     time.Time                 `json:"started_at"`
	CompletedAt   time.Time                 `json:"completed_at"`
	BaseURL       string                    `json:"base_url"`
	Agents        []AgentReport             `json:"agents"`
	Feedback      []FeedbackEntry           `json:"feedback,omitempty"`
	Assertions    []AssertionResult         `json:"assertions"`
	Captures      map[string]map[string]any `json:"captures"`
	Failed        bool                      `json:"failed"`
	FailureReason string                    `json:"failure_reason,omitempty"`
}

type FeedbackEntry struct {
	Agent      string         `json:"agent"`
	Turn       int            `json:"turn"`
	Summary    string         `json:"summary"`
	Details    map[string]any `json:"details,omitempty"`
	RecordedAt time.Time      `json:"recorded_at"`
}

type AgentReport struct {
	Name     string          `json:"name"`
	Username string          `json:"username"`
	Mode     Mode            `json:"mode"`
	Steps    []CommandResult `json:"steps"`
}

type AssertionResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Details  string `json:"details,omitempty"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
}

type CommandResult struct {
	Name      string         `json:"name"`
	Agent     string         `json:"agent"`
	Args      []string       `json:"args"`
	Stdin     map[string]any `json:"stdin,omitempty"`
	ExitCode  int            `json:"exit_code"`
	Succeeded bool           `json:"succeeded"`
	Stdout    string         `json:"stdout"`
	Stderr    string         `json:"stderr"`
}

type DriverRequest struct {
	Scenario       string                    `json:"scenario"`
	RunID          string                    `json:"run_id"`
	Agent          string                    `json:"agent"`
	Objective      string                    `json:"objective"`
	Profile        string                    `json:"profile"`
	Turn           int                       `json:"turn"`
	MaxTurns       int                       `json:"max_turns"`
	Captures       map[string]map[string]any `json:"captures"`
	History        []CommandResult           `json:"history"`
	BaseURL        string                    `json:"base_url"`
	ReferenceSteps []Step                    `json:"reference_steps,omitempty"`
}

type DriverAction struct {
	Action       string         `json:"action"`
	Reason       string         `json:"reason"`
	Name         string         `json:"name"`
	Args         []string       `json:"args"`
	Stdin        map[string]any `json:"stdin"`
	AllowFailure bool           `json:"allow_failure"`
}
