package run

import (
	"context"

	"github.com/dipesh/daycare-photos/internal/app"
	"github.com/spf13/cobra"
)

var (
	configPath string
	dryRun     bool
)

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Download daycare photos and upload them to Google Photos",
	Long:  `Search Gmail for daycare report emails, download images, inject EXIF metadata, and upload to Google Photos.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return app.RunProcess(context.Background(), configPath, dryRun)
	},
}

func init() {
	Cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Find and process images without uploading")
}
