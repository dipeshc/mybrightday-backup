package cmd

import (
	"github.com/dipesh/mybrightday-backup/internal/cmd/config"
	"github.com/dipesh/mybrightday-backup/internal/cmd/download"
	initcmd "github.com/dipesh/mybrightday-backup/internal/cmd/init"
	"github.com/dipesh/mybrightday-backup/internal/cmd/version"
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:           "mbdb",
	Short:         "Back up photos from the MyBrightDay app",
	Long:          `A tool to fetch photos from the MyBrightDay API, inject EXIF metadata, and optionally back them up to Google Photos.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	RootCmd.AddCommand(config.Cmd)
	RootCmd.AddCommand(download.Cmd)
	RootCmd.AddCommand(initcmd.Cmd)
	RootCmd.AddCommand(version.Cmd)
}

func Execute() error {
	return RootCmd.Execute()
}
