package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultipleAMQPEndpointsConfig(t *testing.T) {
	// JSON config simulating multiple AMQP endpoints
	jsonConfig := `{
		"messaging": {
			"enable_realtime_amqp": true,
			"realtime_amqp_endpoints": [
				{
					"name": "analytics",
					"enabled": true,
					"url": "amqp://user:pass@analytics-host:5672/",
					"queue_name": "analytics_feed",
					"publish_partial": false,
					"publish_final": true
				},
				{
					"name": "monitoring",
					"enabled": true,
					"url": "amqps://user:pass@monitoring-host:5671/",
					"queue_name": "monitoring_feed",
					"tls": {
						"enabled": true,
						"skip_verify": true
					}
				}
			]
		}
	}`

	// Create a new Config struct
	var cfg Config

	// Unmarshal JSON into config
	err := json.Unmarshal([]byte(jsonConfig), &cfg)
	require.NoError(t, err)

	// Validate results
	assert.True(t, cfg.Messaging.EnableRealtimeAMQP)
	assert.Len(t, cfg.Messaging.RealtimeEndpoints, 2)

	// Check first endpoint
	ep1 := cfg.Messaging.RealtimeEndpoints[0]
	assert.Equal(t, "analytics", ep1.Name)
	assert.Equal(t, "amqp://user:pass@analytics-host:5672/", ep1.URL)
	assert.Equal(t, "analytics_feed", ep1.QueueName)
	assert.False(t, *ep1.PublishPartial)
	assert.True(t, *ep1.PublishFinal)

	// Check second endpoint
	ep2 := cfg.Messaging.RealtimeEndpoints[1]
	assert.Equal(t, "monitoring", ep2.Name)
	assert.Equal(t, "amqps://user:pass@monitoring-host:5671/", ep2.URL)
	assert.True(t, ep2.TLS.Enabled)
	assert.True(t, ep2.TLS.SkipVerify)
}
