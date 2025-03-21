package alloycli

import (
	"errors"
	"fmt"
	"os"

	"github.com/fatih/color"

	runtime "github.com/grafana/alloy/internal/runtime"
	"github.com/grafana/alloy/syntax/diag"
)

// ValidateConfig validates the given Alloy configuration content.
// It returns detailed diagnostics if the configuration is invalid.
func ValidateConfig(filename string, content []byte) error {
	_, err := runtime.ParseSource(filename, content)
	if err != nil {
		var diags diag.Diagnostics
		if errors.As(err, &diags) {
			p := diag.NewPrinter(diag.PrinterConfig{
				Color:              !color.NoColor,
				ContextLinesBefore: 1,
				ContextLinesAfter:  1,
			})
			_ = p.Fprint(os.Stderr, map[string][]byte{filename: content}, diags)
			return fmt.Errorf("validation failed for %s", filename)
		}
		return fmt.Errorf("error validating %s: %w", filename, err)
	}
	return nil
}
