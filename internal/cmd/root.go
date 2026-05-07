package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/dipesh/daycare-photos/internal/cmd/init"
	"github.com/dipesh/daycare-photos/internal/cmd/run"
	"github.com/dipesh/daycare-photos/internal/cmd/version"
	"github.com/spf13/cobra"
)

// plainHandler is a slog.Handler that writes only the message to the writer,
// with no timestamp or level prefix, for clean human-readable console output.
type plainHandler struct{ w io.Writer }

func (h *plainHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelInfo }
func (h *plainHandler) WithAttrs(_ []slog.Attr) slog.Handler         { return h }
func (h *plainHandler) WithGroup(_ string) slog.Handler              { return h }
func (h *plainHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		fmt.Fprintf(&b, "%v", a.Value.Any())
		return true
	})
	fmt.Fprintln(h.w, b.String())
	return nil
}

var (
	verbose bool
)

var RootCmd = &cobra.Command{
	Use:   "daycare-photos",
	Short: "Automatically download and upload daycare photos",
	Long:  `A tool to download daycare photos from Gmail and upload them to Google Photos with EXIF metadata.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		var handler slog.Handler
		if verbose {
			handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		} else {
			handler = &plainHandler{w: os.Stderr}
		}
		slog.SetDefault(slog.New(handler))
	},
	// Handle case where no subcommand is provided but flags for 'run' are.
	RunE: func(cmd *cobra.Command, args []string) error {
		return run.Cmd.RunE(run.Cmd, args)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

	RootCmd.AddCommand(run.Cmd)
	RootCmd.AddCommand(initcmd.Cmd)
	RootCmd.AddCommand(version.Cmd)
}

func Execute() error {
	return RootCmd.Execute()
}
