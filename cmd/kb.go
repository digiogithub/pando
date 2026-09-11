package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/rag/kb"
)

var kbRelinkForce bool

var kbCmd = &cobra.Command{
	Use:   "kb",
	Short: "Knowledge base maintenance commands",
}

var kbRelinkCmd = &cobra.Command{
	Use:   "relink",
	Short: "Index the [[wiki links]] of knowledge base documents",
	Long: `Rebuild the knowledge base wiki-link graph from the documents already stored.

Documents written before Pando indexed [[wiki links]] keep their content but have
no entry in the link graph, and the filesystem sync will not re-index them because
their files have not changed. This command extracts their links from the stored
content. It costs no embeddings and never rewrites the markdown files.

By default only documents that have no links yet are scanned, so the command is
cheap and safe to repeat. Use --force to drop the whole graph and re-extract every
link, which is what you want after an upgrade that changed how links are parsed.

If another Pando instance is already running for this directory, the rebuild is
forwarded to it over IPC and runs on its writer connection, so this command never
opens a second writer next to it. Otherwise it runs in-process.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !config.Get().KBWikiLinksEnabled() {
			return fmt.Errorf("wiki links are disabled: set Remembrances.KBWikiLinks = true to build the link graph")
		}

		stats, forwarded, err := runKBRelink(cmd.Context(), cwd, kbRelinkForce)
		if err != nil {
			if !forwarded && isDBLockedErr(err) {
				return fmt.Errorf("%w\nanother Pando instance may be writing to the database; stop it and retry", err)
			}
			return err
		}

		if stats.Links == 0 {
			fmt.Println("Knowledge base link graph is already up to date.")
			return nil
		}
		fmt.Printf("Indexed %d link(s) across %d document(s) (%d scanned).\n",
			stats.Links, stats.Documents, stats.Scanned)
		return nil
	},
}

// runKBRelink rebuilds the wiki-link graph for the project at cwd (config
// already loaded). It prefers the running primary (kb.relink RPC, see
// kbRelinkViaRunningInstance) and only relinks in-process when no live
// instance holds the IPC lock. forwarded reports which path ran.
func runKBRelink(ctx context.Context, cwd string, force bool) (protocol.KBRelinkResult, bool, error) {
	if res, forwarded, err := kbRelinkViaRunningInstance(ctx, cwd, force); forwarded {
		return res, true, err
	}

	// No instance running for this directory: relink in-process. ConnectCLI
	// never migrates an existing database from this (possibly different)
	// binary.
	conn, err := db.ConnectCLI()
	if err != nil {
		return protocol.KBRelinkResult{}, false, fmt.Errorf("open database: %w", err)
	}
	defer conn.Close()

	// Link extraction reads stored content and writes kb_links: no embedder,
	// no chunking, so a bare store is enough.
	store := kb.NewKBStore(conn, nil, 0, 0)

	var stats kb.BackfillStats
	if force {
		stats, err = store.RelinkAll(ctx)
	} else {
		stats, err = store.BackfillLinks(ctx)
	}
	return protocol.KBRelinkResult{
		Candidates: stats.Candidates,
		Scanned:    stats.Scanned,
		Documents:  stats.Documents,
		Links:      stats.Links,
	}, false, err
}

func init() {
	kbRelinkCmd.Flags().BoolVar(&kbRelinkForce, "force", false, "rebuild the whole graph instead of only documents with no links")

	kbCmd.AddCommand(kbRelinkCmd)
	rootCmd.AddCommand(kbCmd)
}
