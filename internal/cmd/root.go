package cmd

import (
	"github.com/dipesh/mybrightday-backup/internal/cmd/config"
	"github.com/dipesh/mybrightday-backup/internal/cmd/init"
	"github.com/dipesh/mybrightday-backup/internal/cmd/run"
	"github.com/dipesh/mybrightday-backup/internal/cmd/version"
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "mbdb",
	Short: "Back up photos from the MyBrightDay app",
	Long:  `A tool to fetch photos from the MyBrightDay API, inject EXIF metadata, and optionally back them up to Google Photos.`,
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
