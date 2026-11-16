package mybrightday

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// LoginCommand returns the top-level "login" command. Call
// RootCmd.AddCommand(mybrightday.LoginCommand()) to register it.
func LoginCommand() *cobra.Command {
	var printURL bool
	var callbackURL string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to MyBrightDay and save a token for future runs",
		Long: `Log in to MyBrightDay once so that future runs need no password.

Opens your browser to the Bright Horizons login page and saves a refresh token
to mybrightday/refresh_token in the config directory. The token is rotated
automatically on every run.

Two flags support logging in from a machine without a browser, such as a CI
runner, by splitting the login into two steps: --print-url prints the login
link, and --callback-url completes the login with the address the browser
landed on.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if printURL && callbackURL != "" {
				return errors.New("use either --print-url or --callback-url, not both")
			}
			if printURL {
				return LoginPrintURL(cmd.OutOrStdout())
			}
			if callbackURL != "" {
				return friendlyLoginError(LoginFinish(cmd.Context(), callbackURL))
			}
			return friendlyLoginError(LoginInteractive(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout()))
		},
	}

	cmd.Flags().BoolVar(&printURL, "print-url", false, "Print the login link and exit; finish later with --callback-url")
	cmd.Flags().StringVar(&callbackURL, "callback-url", "", "The address the browser landed on after logging in (starts with http://localhost:4200/okta/callback?code=)")
	return cmd
}

// friendlyLoginError turns the login sentinel errors into messages that say
// what happened and what to do, without any internals.
func friendlyLoginError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNoPendingLogin):
		return errors.New("no login is in progress on this machine.\nRun `mbdb login --print-url` first, then pass the address you land on to --callback-url")
	case errors.Is(err, ErrCallbackNoCode):
		return errors.New("the address you pasted doesn't contain a login code.\nExpected an address starting with http://localhost:4200/okta/callback?code=\nStart again to get a fresh login link")
	case errors.Is(err, ErrCallbackStateMismatch):
		return errors.New("the address you pasted doesn't match the login link this machine issued.\nIf you started the login more than once, use the most recent link. Otherwise start again to get a fresh one")
	case errors.Is(err, ErrLoginLinkExpired):
		return errors.New("the login link has expired or was already used. Links last ten minutes.\nStart again to get a fresh one")
	default:
		return fmt.Errorf("MyBrightDay login failed: %w", err)
	}
}
