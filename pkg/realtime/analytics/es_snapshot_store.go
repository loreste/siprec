package analytics

import (
	"context"

	"siprec-server/pkg/elasticsearch"
)

// ElasticsearchSnapshotWriter persists analytics snapshots to Elasticsearch.
type ElasticsearchSnapshotWriter struct {
	client *elasticsearch.Client
	index  string
}

// NewElasticsearchSnapshotWriter constructs a new snapshot writer.
func NewElasticsearchSnapshotWriter(client *elasticsearch.Client, index string) *ElasticsearchSnapshotWriter {
	return &ElasticsearchSnapshotWriter{client: client, index: index}
}

// Save writes the snapshot to Elasticsearch.
func (w *ElasticsearchSnapshotWriter) Save(ctx context.Context, snapshot *AnalyticsSnapshot) error {
	if snapshot == nil {
		return nil
	}
	id := snapshot.CallID
	if id == "" {
		id = "unknown"
	}
	return w.client.IndexDocument(ctx, w.index, id, snapshot)
}
