package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
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
link, which is what you want after an upgrade that changed how links are parsed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		conn, err := db.Connect()
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer conn.Close()

		// Link extraction reads stored content and writes kb_links: no embedder,
		// no chunking, so a bare store is enough.
		store := kb.NewKBStore(conn, nil, 0, 0)

		var stats kb.BackfillStats
		if kbRelinkForce {
			stats, err = store.RelinkAll(cmd.Context())
		} else {
			stats, err = store.BackfillLinks(cmd.Context())
		}
		if err != nil {
			if isDBLockedErr(err) {
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

func init() {
	kbRelinkCmd.Flags().BoolVar(&kbRelinkForce, "force", false, "rebuild the whole graph instead of only documents with no links")

	kbCmd.AddCommand(kbRelinkCmd)
	rootCmd.AddCommand(kbCmd)
}
