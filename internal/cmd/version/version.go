package version

import (
	"fmt"

	"github.com/dipesh/daycare-photos/internal/app"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of daycare-photos",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("daycare-photos %s\n", app.Version)
	},
}
