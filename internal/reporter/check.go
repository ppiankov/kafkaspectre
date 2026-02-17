package reporter

import "context"

// CheckStatus describes how a topic reference compares between repo and cluster.
type CheckStatus string

const (
	CheckStatusOK                 CheckStatus = "OK"
	CheckStatusMissingInCluster   CheckStatus = "MISSING_IN_CLUSTER"
	CheckStatusUnreferencedInRepo CheckStatus = "UNREFERENCED_IN_REPO"
	CheckStatusUnused             CheckStatus = "UNUSED"
)

// CheckReference is a single repository reference to a topic.
type CheckReference struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Source string `json:"source"`
}

// CheckFinding contains comparison details for one topic.
type CheckFinding struct {
	Topic            string           `json:"topic"`
	Status           CheckStatus      `json:"status"`
	ReferencedInRepo bool             `json:"referenced_in_repo"`
	InCluster        bool             `json:"in_cluster"`
	ConsumerGroups   []string         `json:"consumer_groups,omitempty"`
	References       []CheckReference `json:"references,omitempty"`
	Reason           string           `json:"reason"`
}

// CheckSummary contains high-level check counters.
type CheckSummary struct {
	RepoPath                string `json:"repo_path"`
	FilesScanned            int    `json:"files_scanned"`
	RepoTopics              int    `json:"repo_topics"`
	ClusterTopics           int    `json:"cluster_topics"`
	TotalFindings           int    `json:"total_findings"`
	OKCount                 int    `json:"ok_count"`
	MissingInClusterCount   int    `json:"missing_in_cluster_count"`
	UnreferencedInRepoCount int    `json:"unreferenced_in_repo_count"`
	UnusedCount             int    `json:"unused_count"`
}

// CheckResult is the full output model for the check command.
type CheckResult struct {
	Summary  *CheckSummary   `json:"summary"`
	Findings []*CheckFinding `json:"findings"`
}

// CheckReporter generates check command output.
type CheckReporter interface {
	GenerateCheck(ctx context.Context, result *CheckResult) error
}
