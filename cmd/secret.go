package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/spf13/cobra"
)

var secretCmd = &cobra.Command{
	Use:   "secret <value>",
	Short: "Encrypt or decrypt a secret with Pando AGE keys",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := config.TransformSecretString(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, result)
		return nil
	},
}

func init() {
	secretCmd.Example = strings.TrimSpace(`
  pando secret my-token
  pando secret 'age1:YWdlLWVuY3J5cHRlZC1wYXlsb2Fk'
`)
	rootCmd.AddCommand(secretCmd)
}
