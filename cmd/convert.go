package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/convert"
)

var (
	convertOutput      string
	convertListFormats bool
)

var convertCmd = &cobra.Command{
	Use:   "convert [input]",
	Short: "Convert a document (docx, pdf, xlsx, …) or URL to Markdown",
	Long: `Convert a rich document or web page to Markdown.

Supported inputs include Word (.docx), PDF (.pdf), Excel (.xlsx/.xls),
PowerPoint (.pptx), HTML, CSV, EPUB, Jupyter notebooks and more, as well as
http(s) URLs.

By default the Markdown is printed to stdout so it can be piped or redirected.
Use -o/--output to write it to a file instead.

Examples:
  pando convert report.docx
  pando convert data.xlsx -o data.md
  pando convert https://example.com/page.html
  pando convert --list-formats`,
	Args: func(cmd *cobra.Command, args []string) error {
		if convertListFormats {
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

func isURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func init() {
	convertCmd.Flags().StringVarP(&convertOutput, "output", "o", "", "write Markdown to this file instead of stdout")
	convertCmd.Flags().BoolVar(&convertListFormats, "list-formats", false, "list supported input formats and exit")
	rootCmd.AddCommand(convertCmd)
}
