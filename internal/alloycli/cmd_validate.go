package alloycli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/fatih/color"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/grafana/alloy/internal/featuregate"
	alloy_runtime "github.com/grafana/alloy/internal/runtime"
	"github.com/grafana/alloy/internal/runtime/logging"
	"github.com/grafana/alloy/internal/runtime/tracing"
	"github.com/grafana/alloy/internal/service"
	"github.com/grafana/alloy/internal/service/labelstore"
	"github.com/grafana/alloy/internal/service/livedebugging"
	otel_service "github.com/grafana/alloy/internal/service/otel"
	"github.com/grafana/alloy/syntax/diag"
	"github.com/grafana/alloy/syntax/parser"
	"github.com/grafana/alloy/syntax/printer"
)

func validateCommand() *cobra.Command {
	r := &alloyValidate{
		format: false,
	}

	cmd := &cobra.Command{
		Use:   "validate [flags] path...",
		Short: "Validate Alloy configuration files",
		Long: `The validate subcommand checks the syntax and semantics of Alloy configuration files.

The command accepts one or more paths as arguments. Each path can be a file or
a directory. If a path is a directory, all *.alloy files in that directory will
be validated. Subdirectories are not recursively searched.

The validate command only checks configuration files; it does not run them.
If any configuration file contains errors, the command will exit with a non-zero
status code and display the errors.
`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.Run(cmd, args)
		},
	}

	cmd.Flags().BoolVarP(&r.format, "format", "f", r.format, "Format file once is valid")

	return cmd
}

type alloyValidate struct {
	format bool
}

func (v *alloyValidate) Run(cmd *cobra.Command, paths []string) error {
	var hasErrors bool

	for _, path := range paths {
		fi, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", path, err)
			hasErrors = true
			continue
		}

		if fi.IsDir() {
			files, err := filepath.Glob(filepath.Join(path, "*.alloy"))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error scanning directory %s: %v\n", path, err)
				hasErrors = true
				continue
			}

			for _, file := range files {
				if err := validateFile(file, v.format); err != nil {
					hasErrors = true
				}
			}
		} else {
			if err := validateFile(path, v.format); err != nil {
				hasErrors = true
			}
		}
	}

	if hasErrors {
		return fmt.Errorf("validation failed")
	}

	fmt.Println("All configuration files are valid.")
	return nil
}

func validateFile(path string, formatIfValid bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", path, err)
		return err
	}

	// Parse the file to validate basic syntax
	source, err := alloy_runtime.ParseSource(path, content)
	if err != nil {
		var diags diag.Diagnostics
		if errors.As(err, &diags) {
			p := diag.NewPrinter(diag.PrinterConfig{
				Color:              !color.NoColor,
				ContextLinesBefore: 1,
				ContextLinesAfter:  1,
			})
			_ = p.Fprint(os.Stderr, map[string][]byte{path: content}, diags)
			return fmt.Errorf("validation failed for %s", path)
		}
		fmt.Fprintf(os.Stderr, "Error validating %s: %v\n", path, err)
		return err
	}

	// Use a silent logger to avoid cluttering output
	logger := logging.NewNop()

	// Create tracer
	tracer, err := tracing.New(tracing.DefaultOptions)
	if err != nil {
		return fmt.Errorf("failed to create tracer: %w", err)
	}

	// Create necessary services
	liveDebuggingService := livedebugging.New()
	labelService := labelstore.New(logger, prometheus.NewRegistry())
	otelService := otel_service.New(logger)

	// Create a controller with the same settings as the run command
	controller := alloy_runtime.New(alloy_runtime.Options{
		Logger:       logger,
		Tracer:       tracer,
		MinStability: featuregate.StabilityGenerallyAvailable,
		// Include services that components might need
		Services: []service.Service{
			liveDebuggingService,
			labelService,
			otelService,
		},
	})

	// Validate component references by loading the configuration
	err = controller.LoadSource(source, nil, path)
	if err != nil {
		var diags diag.Diagnostics
		if errors.As(err, &diags) {
			p := diag.NewPrinter(diag.PrinterConfig{
				Color:              !color.NoColor,
				ContextLinesBefore: 1,
				ContextLinesAfter:  1,
			})
			_ = p.Fprint(os.Stderr, map[string][]byte{path: content}, diags)
			return fmt.Errorf("component validation failed for %s", path)
		}
		fmt.Fprintf(os.Stderr, "Error validating components in %s: %v\n", path, err)
		return err
	}

	// If requested and validation passed, format the file
	if formatIfValid {
		if err := formatFile(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting %s: %v\n", path, err)
			return err
		}
		fmt.Printf("%s: OK (formatted)\n", path)
	} else {
		fmt.Printf("%s: OK\n", path)
	}

	return nil
}

// formatFile formats a file in-place using the same formatting logic as the fmt command
func formatFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	f, err := parser.ParseFile(path, content)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, f); err != nil {
		return err
	}

	// Add a newline at the end of the file
	_, _ = buf.Write([]byte{'\n'})

	// Check if the file is already formatted correctly
	if reflect.DeepEqual(content, buf.Bytes()) {
		return nil // No changes needed
	}

	// Write the formatted content back to the file
	wf, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, fi.Mode().Perm())
	if err != nil {
		return err
	}
	defer wf.Close()

	_, err = io.Copy(wf, &buf)
	return err
}

// silentLogger is a minimal logger implementation that discards all logs
// Used during validation to avoid cluttering output with log messages
type silentLogger struct{}

func (l *silentLogger) Log(keyvals ...interface{}) error {
	return nil
}
