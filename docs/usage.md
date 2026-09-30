# Command-line usage

```bash
# Start Pando
pando

# Start Pando as an MCP server (stdio + HTTP /mcp)
pando mcp-server

# Start with debug logging
pando -d

# Start with a specific working directory
pando -c /path/to/project

# Run a single prompt in non-interactive mode
pando -p "Explain the use of context in Go"

# Get response in JSON format
pando -p "Explain the use of context in Go" -f json

# Check for a newer compatible GitHub release
pando update --check

# Update the current binary in place
pando update

# Disable one MCP transport if needed
pando mcp-server --no-stdio
pando mcp-server --no-http

# Convert a document (docx, pdf, xlsx, pptx, html, csv, epub, …) to Markdown
pando convert report.docx              # prints Markdown to stdout
pando convert data.xlsx -o data.md     # writes to a file
pando convert https://example.com/page # converts a web page
pando convert --list-formats           # list supported input formats

# Remote diagnostics: off by default, see docs/telemetry.md
pando telemetry status
pando telemetry enable
```

When Pando starts from a released semantic-version build, it also performs a short background
update check and prints a notice if a newer compatible release is available.
