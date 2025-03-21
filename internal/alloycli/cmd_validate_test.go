package alloycli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCommand(t *testing.T) {
	// Create the validate command
	cmd := validateCommand()

	// Test command properties
	require.Equal(t, "validate [flags] path...", cmd.Use)
	require.NotEmpty(t, cmd.Short)
	require.NotEmpty(t, cmd.Long)

	// Verify the format flag exists
	formatFlag := cmd.Flag("format")
	require.NotNil(t, formatFlag)
	require.Equal(t, "f", formatFlag.Shorthand)
}

func TestValidateCommandRun(t *testing.T) {
	// Create temp directory for test files
	tmpDir, err := os.MkdirTemp("", "alloy-validate-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create valid file
	validPath := filepath.Join(tmpDir, "valid.alloy")
	validContent := `
	logging {
	level  = "info"
	format = "logfmt"
	}
	`
	err = os.WriteFile(validPath, []byte(validContent), 0644)
	require.NoError(t, err)

	// Create invalid file
	invalidPath := filepath.Join(tmpDir, "invalid.alloy")
	invalidContent := `
	logging {
	logLevel  = "info"
	format = "logfmt"
	}
	`
	err = os.WriteFile(invalidPath, []byte(invalidContent), 0644)
	require.NoError(t, err)

	// Run tests with the complete command
	tests := []struct {
		name          string
		args          []string
		format        bool
		expectSuccess bool
	}{
		{
			name:          "valid file without format",
			args:          []string{validPath},
			format:        false,
			expectSuccess: true,
		},
		{
			name:          "valid file with format",
			args:          []string{validPath},
			format:        true,
			expectSuccess: true,
		},
		{
			name:          "invalid file",
			args:          []string{invalidPath},
			format:        false,
			expectSuccess: false,
		},
		{
			name:          "mixed valid and invalid",
			args:          []string{validPath, invalidPath},
			format:        false,
			expectSuccess: false,
		},
		{
			name:          "directory with mixed content",
			args:          []string{tmpDir},
			format:        false,
			expectSuccess: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := validateCommand()

			if tc.format {
				err := cmd.Flags().Set("format", "true")
				require.NoError(t, err)
			}

			// Create a validator instance from the command
			v := &alloyValidate{format: tc.format}

			err := v.Run(cmd, tc.args)

			if tc.expectSuccess {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestFormatPreservation(t *testing.T) {
	// Create a temporary file with content to be formatted
	tmpDir, err := os.MkdirTemp("", "alloy-format-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test file with specific formatting
	testFile := filepath.Join(tmpDir, "config.alloy")
	content := `
	logging { // Comment at block start
	/* Multi-line
		comment */
	level  = "info"
	format = "logfmt" // Inline comment
	}
	`
	err = os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	// Create the command and set format flag
	cmd := validateCommand()
	err = cmd.Flags().Set("format", "true")
	require.NoError(t, err)

	// Create a validator and run it
	v := &alloyValidate{format: true}
	err = v.Run(cmd, []string{testFile})
	require.NoError(t, err)

	// Read formatted content
	formatted, err := os.ReadFile(testFile)
	require.NoError(t, err)

	// Verify that comments are preserved
	formattedStr := string(formatted)
	require.Contains(t, formattedStr, "// Comment at block start")
	require.Contains(t, formattedStr, "/* Multi-line")
	require.Contains(t, formattedStr, "comment */")
	require.Contains(t, formattedStr, "// Inline comment")
}
