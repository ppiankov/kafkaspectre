package reporter

import (
	"context"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// Reporter generates reports from Kafka metadata
type Reporter interface {
	Generate(ctx context.Context, metadata *kafka.ClusterMetadata) error
	GenerateAudit(ctx context.Context, result *AuditResult) error
}
