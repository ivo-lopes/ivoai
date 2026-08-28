// Package workingcontext defines transient, provider-neutral evidence produced
// during one managed IVOAI session. It is not durable memory or Context/RAG.
package workingcontext

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	MaxSummaryBytes       = 16 << 10
	MaxFindingSummary     = 4 << 10
	MaxFindings           = 64
	MaxResultRefs         = 64
	MaxStateChanges       = 64
	MaxImportantErrors    = 16
	MaxImportantErrorSize = 2 << 10
)

var (
	opaqueIDPattern = regexp.MustCompile(`^artifact_[0-9a-f]{32}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	labelPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
)

type Sensitivity string

const (
	SensitivityInternal   Sensitivity = "internal"
	SensitivityRestricted Sensitivity = "restricted"
	SensitivitySecret     Sensitivity = "secret"
	SensitivityCredential Sensitivity = "credential"
	SensitivityRawAuth    Sensitivity = "raw_auth"
)

func (s Sensitivity) Persistable() bool {
	return s == SensitivityInternal || s == SensitivityRestricted
}

type Ownership struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id,omitempty"`
	WorkerID  string `json:"worker_id,omitempty"`
}

func (o Ownership) Validate() error {
	if len(o.SessionID) != 37 || !strings.HasPrefix(o.SessionID, "sess_") || !labelPattern.MatchString(o.SessionID) {
		return errors.New("working context requires a valid session owner")
	}
	if o.TaskID != "" && !labelPattern.MatchString(o.TaskID) {
		return errors.New("working context task owner is invalid")
	}
	if o.WorkerID != "" && (len(o.WorkerID) != 39 || !strings.HasPrefix(o.WorkerID, "worker_") || !labelPattern.MatchString(o.WorkerID)) {
		return errors.New("working context worker owner is invalid")
	}
	return nil
}

type ArtifactKind string

const (
	ArtifactWorkerOutput ArtifactKind = "worker_output"
	ArtifactStdout       ArtifactKind = "stdout"
	ArtifactStderr       ArtifactKind = "stderr"
	ArtifactTestLog      ArtifactKind = "test_log"
	ArtifactDiff         ArtifactKind = "diff"
	ArtifactBinary       ArtifactKind = "binary"
)

type ArtifactRef struct {
	ID          string       `json:"id"`
	Kind        ArtifactKind `json:"kind"`
	Size        int64        `json:"size"`
	SHA256      string       `json:"sha256"`
	MediaType   string       `json:"media_type"`
	CreatedAt   time.Time    `json:"created_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
	Owner       Ownership    `json:"owner"`
	Sensitivity Sensitivity  `json:"sensitivity"`
	Complete    bool         `json:"complete"`
	Truncated   bool         `json:"truncated,omitempty"`
}

func (r ArtifactRef) Validate() error {
	if !opaqueIDPattern.MatchString(r.ID) || !digestPattern.MatchString(r.SHA256) {
		return errors.New("artifact reference identity or digest is invalid")
	}
	if !validKind(r.Kind) || r.Size < 0 || r.MediaType == "" || len(r.MediaType) > 128 || strings.ContainsAny(r.MediaType, "\x00\r\n\x1b") {
		return errors.New("artifact reference metadata is invalid")
	}
	if r.CreatedAt.IsZero() || !r.ExpiresAt.After(r.CreatedAt) || !r.Sensitivity.Persistable() || (r.Complete && r.Truncated) {
		return errors.New("artifact reference lifecycle is invalid")
	}
	return r.Owner.Validate()
}

type EvidenceRole string

const (
	EvidencePrimary EvidenceRole = "primary"
	EvidenceFinding EvidenceRole = "finding"
	EvidenceFailure EvidenceRole = "failure"
)

type ResultRef struct {
	Artifact ArtifactRef  `json:"artifact"`
	Role     EvidenceRole `json:"role"`
}

func (r ResultRef) Validate() error {
	if r.Role != EvidencePrimary && r.Role != EvidenceFinding && r.Role != EvidenceFailure {
		return errors.New("result reference role is invalid")
	}
	return r.Artifact.Validate()
}

type ResultStatus string

const (
	ResultCompleted ResultStatus = "completed"
	ResultFailed    ResultStatus = "failed"
	ResultCancelled ResultStatus = "cancelled"
	ResultDegraded  ResultStatus = "degraded"
)

type Importance string

const (
	ImportanceInfo     Importance = "info"
	ImportanceLow      Importance = "low"
	ImportanceModerate Importance = "moderate"
	ImportanceHigh     Importance = "high"
	ImportanceCritical Importance = "critical"
)

type Location struct {
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type Finding struct {
	Category   string      `json:"category"`
	Importance Importance  `json:"importance"`
	Summary    string      `json:"summary"`
	Location   Location    `json:"location,omitempty"`
	Evidence   []ResultRef `json:"evidence,omitempty"`
}

func (f Finding) Validate() error {
	if !labelPattern.MatchString(f.Category) || !validImportance(f.Importance) || !boundedText(f.Summary, MaxFindingSummary) || len(f.Evidence) > MaxResultRefs {
		return errors.New("finding is invalid or exceeds its bound")
	}
	if f.Location.Path != "" && (len(f.Location.Path) > 1024 || strings.HasPrefix(f.Location.Path, "/") || strings.Contains(f.Location.Path, "..") || strings.ContainsAny(f.Location.Path, "\x00\r\n\x1b")) {
		return errors.New("finding location must be a bounded relative hint")
	}
	if f.Location.Line < 0 || f.Location.Column < 0 {
		return errors.New("finding location is invalid")
	}
	for _, evidence := range f.Evidence {
		if err := evidence.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type ChangeKind string

const (
	ChangeFile        ChangeKind = "file"
	ChangeTest        ChangeKind = "test"
	ChangeDecision    ChangeKind = "decision"
	ChangeBlocker     ChangeKind = "blocker"
	ChangeObservation ChangeKind = "observation"
)

type ProposedChange struct {
	Kind     ChangeKind  `json:"kind"`
	Target   string      `json:"target,omitempty"`
	Summary  string      `json:"summary"`
	Evidence []ResultRef `json:"evidence,omitempty"`
}

type StateDelta struct {
	Proposed []ProposedChange `json:"proposed,omitempty"`
}

func (d StateDelta) Validate() error {
	if len(d.Proposed) > MaxStateChanges {
		return errors.New("state delta exceeds its bound")
	}
	for _, change := range d.Proposed {
		if !validChangeKind(change.Kind) || !boundedText(change.Summary, MaxFindingSummary) || len(change.Target) > 1024 || strings.ContainsAny(change.Target, "\x00\r\n\x1b") || len(change.Evidence) > MaxResultRefs {
			return errors.New("state delta contains an invalid proposal")
		}
		for _, evidence := range change.Evidence {
			if err := evidence.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

type WorkerResult struct {
	Status          ResultStatus `json:"status"`
	Summary         string       `json:"summary"`
	Findings        []Finding    `json:"findings,omitempty"`
	Evidence        []ResultRef  `json:"evidence"`
	StateDelta      StateDelta   `json:"state_delta"`
	ImportantErrors []string     `json:"important_errors,omitempty"`
	Degraded        bool         `json:"degraded,omitempty"`
	Truncated       bool         `json:"truncated,omitempty"`
}

func (r WorkerResult) Validate() error {
	if !validResultStatus(r.Status) || len(r.Summary) > MaxSummaryBytes || strings.ContainsRune(r.Summary, '\x00') || len(r.Findings) > MaxFindings || len(r.Evidence) > MaxResultRefs || len(r.ImportantErrors) > MaxImportantErrors {
		return errors.New("worker result is invalid or exceeds its bound")
	}
	for _, finding := range r.Findings {
		if err := finding.Validate(); err != nil {
			return err
		}
	}
	for _, evidence := range r.Evidence {
		if err := evidence.Validate(); err != nil {
			return err
		}
	}
	if err := r.StateDelta.Validate(); err != nil {
		return err
	}
	for _, important := range r.ImportantErrors {
		if !boundedText(important, MaxImportantErrorSize) {
			return fmt.Errorf("important error exceeds its bound")
		}
	}
	return nil
}

func validKind(value ArtifactKind) bool {
	switch value {
	case ArtifactWorkerOutput, ArtifactStdout, ArtifactStderr, ArtifactTestLog, ArtifactDiff, ArtifactBinary:
		return true
	}
	return false
}

func validImportance(value Importance) bool {
	switch value {
	case ImportanceInfo, ImportanceLow, ImportanceModerate, ImportanceHigh, ImportanceCritical:
		return true
	}
	return false
}

func validChangeKind(value ChangeKind) bool {
	switch value {
	case ChangeFile, ChangeTest, ChangeDecision, ChangeBlocker, ChangeObservation:
		return true
	}
	return false
}

func validResultStatus(value ResultStatus) bool {
	switch value {
	case ResultCompleted, ResultFailed, ResultCancelled, ResultDegraded:
		return true
	}
	return false
}

func boundedText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && !strings.ContainsRune(value, '\x00')
}
