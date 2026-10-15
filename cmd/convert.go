package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/convert"
)

var (
	convertOutput      string
	convertListFormats bool
	convertStdinFormat string
)

// maxStdinDocument bounds a document read from stdin with --stdin-format.
const maxStdinDocument = 256 << 20

var convertCmd = &cobra.Command{
	Use:   "convert [input]",
	Short: "Convert a document (docx, pdf, xlsx, …) or URL to Markdown",
	Long: `Convert a rich document or web page to Markdown.

Supported inputs include Word (.docx), PDF (.pdf), Excel (.xlsx/.xls),
PowerPoint (.pptx), HTML, CSV, EPUB, Jupyter notebooks and more, as well as
http(s) URLs.

By default the Markdown is printed to stdout so it can be piped or redirected.
Use -o/--output to write it to a file instead.

With --stdin-format the document is read from stdin instead (no file or URL
argument); the flag names its format as an extension. The HTTP conversion
endpoint (pando llm-proxy --convert) runs each conversion this way in a child
process.

Examples:
  pando convert report.docx
  cat report.pdf | pando convert --stdin-format pdf
  pando convert data.xlsx -o data.md
  pando convert https://example.com/page.html
  pando convert --list-formats`,
	Args: func(cmd *cobra.Command, args []string) error {
		if convertListFormats {
			return nil
		}
		if convertStdinFormat != "" {
			if len(args) != 0 {
				return fmt.Errorf("--stdin-format reads stdin and takes no input argument")
			}
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("requires exactly one input file or URL")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if convertListFormats {
			fmt.Fprintln(cmd.OutOrStdout(), "Supported input extensions:")
			fmt.Fprintln(cmd.OutOrStdout(), "  "+strings.Join(convert.SupportedExtensions(), " "))
			return nil
		}

		if convertStdinFormat != "" {
			return convertStdin(cmd, convertStdinFormat)
		}

		input := args[0]
		c := convert.New()

		var (
			md  string
			err error
		)
		if isURL(input) {
			md, err = c.Convert(input)
		} else {
			if _, statErr := os.Stat(input); statErr != nil {
				return fmt.Errorf("convert: cannot access %q: %w", input, statErr)
			}
			if !convert.IsSupported(input) {
				return fmt.Errorf("convert: unsupported file format; run 'pando convert --list-formats' to see supported types")
			}
			md, err = c.ConvertFile(input)
		}
		if err != nil {
			return err
		}

		if strings.TrimSpace(convertOutput) != "" {
			if writeErr := os.WriteFile(convertOutput, []byte(md), 0o644); writeErr != nil {
				return fmt.Errorf("convert: write output %q: %w", convertOutput, writeErr)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s (%d bytes)\n", convertOutput, len(md))
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), md)
		return nil
	},
}

// convertStdin converts the document on stdin, whose format is the
// extension ext (with or without the dot), and writes the Markdown like a
// file conversion does. It never fetches URLs.
func convertStdin(cmd *cobra.Command, ext string) error {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	name := "stdin" + ext
	if !convert.IsSupported(name) {
		return fmt.Errorf("convert: unsupported format %q; run 'pando convert --list-formats' to see supported types", ext)
	}
	data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxStdinDocument+1))
	if err != nil {
		return fmt.Errorf("convert: read stdin: %w", err)
	}
	if len(data) > maxStdinDocument {
		return fmt.Errorf("convert: stdin document exceeds %d bytes", maxStdinDocument)
	}
	md, err := convert.New().ConvertReader(bytes.NewReader(data), name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(convertOutput) != "" {
		if writeErr := os.WriteFile(convertOutput, []byte(md), 0o644); writeErr != nil {
			return fmt.Errorf("convert: write output %q: %w", convertOutput, writeErr)
		}
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), md)
	return nil
}

func isURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func init() {
	convertCmd.Flags().StringVarP(&convertOutput, "output", "o", "", "write Markdown to this file instead of stdout")
	convertCmd.Flags().BoolVar(&convertListFormats, "list-formats", false, "list supported input formats and exit")
	convertCmd.Flags().StringVar(&convertStdinFormat, "stdin-format", "", "read the document from stdin; its format as an extension (pdf, docx, …)")
	rootCmd.AddCommand(convertCmd)
}
