package version

import (
	"fmt"

	"github.com/dipesh/mybrightday-backup/internal/app"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("mbdb %s\n", app.Version)
		return nil
	},
}
