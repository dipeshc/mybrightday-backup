package cmd

import (
	"github.com/dipesh/mybrightday-photos-downloader/internal/cmd/config"
	"github.com/dipesh/mybrightday-photos-downloader/internal/cmd/init"
	"github.com/dipesh/mybrightday-photos-downloader/internal/cmd/run"
	"github.com/dipesh/mybrightday-photos-downloader/internal/cmd/version"
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "mybrightday-photos-downloader",
	Short: "Download photos from the MyBrightDay API",
	Long:  `A tool to fetch photos from the MyBrightDay API and save them locally with EXIF metadata. Optionally upload to Google Photos.`,
	// Handle case where no subcommand is provided but flags for 'run' are.
	RunE: func(cmd *cobra.Command, args []string) error {
		return run.Cmd.RunE(run.Cmd, args)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	RootCmd.AddCommand(config.Cmd)
	RootCmd.AddCommand(run.Cmd)
	RootCmd.AddCommand(initcmd.Cmd)
	RootCmd.AddCommand(version.Cmd)
}

func Execute() error {
	return RootCmd.Execute()
}
