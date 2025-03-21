package alloycli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/fatih/color"
	"github.com/grafana/alloy/internal/runtime"
	"github.com/grafana/alloy/syntax/diag"
	"github.com/grafana/alloy/syntax/parser"
	"github.com/grafana/alloy/syntax/printer"
	"github.com/spf13/cobra"
)

func fmtCommand() *cobra.Command {
	f := &alloyFmt{
		write: false,
		test:  false,
	}

	cmd := &cobra.Command{
		Use:   "fmt [flags] file",
		Short: "Format a configuration file",
		Long: `The fmt subcommand applies standard formatting rules to the
specified configuration file.

If the file argument is not supplied or if the file argument is "-", then fmt will read from stdin.

The -w flag can be used to write the formatted file back to disk. -w can not be provided when fmt is reading from stdin. When -w is not provided, fmt will write the result to stdout.`,
		Args:         cobra.RangeArgs(0, 1),
		SilenceUsage: true,
		Aliases:      []string{"format"},

		RunE: func(_ *cobra.Command, args []string) error {
			var err error

			if len(args) == 0 {
				// Read from stdin when there are no args provided.
				err = f.Run("-")
			} else {
				err = f.Run(args[0])
			}

			var diags diag.Diagnostics
			if errors.As(err, &diags) {
				for _, diag := range diags {
					fmt.Fprintln(os.Stderr, diag)
				}
				return fmt.Errorf("encountered errors during formatting")
			}

			if f.validate {
				// Validate configuration without modifying it
				filename := "-"
				if len(args) > 0 {
					filename = args[0]
				}

				// Read the file content
				var content []byte
				if filename == "-" {
					content, err = io.ReadAll(os.Stdin)
					if err != nil {
						return fmt.Errorf("error reading from stdin: %w", err)
					}
				} else {
					content, err = os.ReadFile(filename)
					if err != nil {
						return fmt.Errorf("error reading %s: %w", filename, err)
					}
				}

				// Create a map with just the file being validated
				contents := map[string][]byte{filename: content}

				for file, content := range contents {
					// Use the same loading mechanism as in cmd_run.go, but don't run the configuration
					_, err := runtime.ParseSource(file, content)
					if err != nil {
						var diags diag.Diagnostics
						if errors.As(err, &diags) {
							p := diag.NewPrinter(diag.PrinterConfig{
								Color:              !color.NoColor,
								ContextLinesBefore: 1,
								ContextLinesAfter:  1,
							})
							_ = p.Fprint(os.Stderr, map[string][]byte{file: content}, diags)
							return fmt.Errorf("validation failed for %s", file)
						}
						return fmt.Errorf("error validating %s: %w", file, err)
					}
				}
				fmt.Println("Configuration validation successful!")
				return nil
			}
			return err
		},
	}

	cmd.Flags().BoolVarP(&f.write, "write", "w", f.write, "write result to (source) file instead of stdout")
	cmd.Flags().BoolVarP(&f.test, "test", "t", f.test, "exit with non-zero when changes would be made. Cannot be used with -w/--write")
	cmd.Flags().BoolVar(&f.validate, "validate", false, "Validate configuration without formatting")
	return cmd
}

type alloyFmt struct {
	write    bool
	test     bool
	validate bool
}

func (ff *alloyFmt) Run(configFile string) error {
	if ff.write && ff.test {
		return fmt.Errorf("Cannot use -w/--write and -t/--test at the same time")
	}

	switch configFile {
	case "-":
		if ff.write {
			return fmt.Errorf("cannot use -w with standard input")
		}
		return format("<stdin>", nil, os.Stdin, false, ff.test)

	default:
		fi, err := os.Stat(configFile)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return fmt.Errorf("cannot format a directory")
		}

		f, err := os.Open(configFile)
		if err != nil {
			return err
		}
		defer f.Close()
		return format(configFile, fi, f, ff.write, ff.test)
	}
}

func format(filename string, fi os.FileInfo, r io.Reader, write bool, test bool) error {
	bb, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	f, err := parser.ParseFile(filename, bb)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, f); err != nil {
		return err
	}

	// Add a newline at the end of the file.
	_, _ = buf.Write([]byte{'\n'})

	// If -t/--test flag is check, only check if file is formatted correctly
	if test {
		if !reflect.DeepEqual(bb, buf.Bytes()) {
			return fmt.Errorf("File %s is not formatted correctly", filename)
		}
		return nil
	}

	if !write {
		_, err := io.Copy(os.Stdout, &buf)
		return err
	}

	wf, err := os.OpenFile(filename, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, fi.Mode().Perm())
	if err != nil {
		return err
	}
	defer wf.Close()

	_, err = io.Copy(wf, &buf)
	return err
}
