package config

import (
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataCompatibilityConfiguration(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	for _, format := range []struct{ ext, content string }{
		{"yaml", "network:\n  fill_missing_siprec_metadata: true\n  allow_missing_siprec_participants: true\n"},
		{"json", `{"network":{"fill_missing_siprec_metadata":true,"allow_missing_siprec_participants":true}}`},
	} {
		t.Run(format.ext, func(t *testing.T) {
			t.Setenv("SIPREC_FILL_MISSING_METADATA", "")
			t.Setenv("SIPREC_ALLOW_MISSING_PARTICIPANTS", "")
			path := filepath.Join(t.TempDir(), "config."+format.ext)
			require.NoError(t, os.WriteFile(path, []byte(format.content), 0o600))
			cfg, err := LoadFromFile(logger, path)
			require.NoError(t, err)
			require.True(t, cfg.Network.FillMissingSIPRECMetadata)
			require.True(t, cfg.Network.AllowMissingSIPRECParticipants)
			t.Setenv("SIPREC_FILL_MISSING_METADATA", "false")
			t.Setenv("SIPREC_ALLOW_MISSING_PARTICIPANTS", "false")
			cfg, err = LoadFromFile(logger, path)
			require.NoError(t, err)
			require.False(t, cfg.Network.FillMissingSIPRECMetadata)
			require.False(t, cfg.Network.AllowMissingSIPRECParticipants)
		})
	}
	require.False(t, newDefaultConfig().Network.FillMissingSIPRECMetadata)
	require.False(t, newDefaultConfig().Network.AllowMissingSIPRECParticipants)
}
