package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ncx-ai/keld-cli/internal/auth"
	"github.com/ncx-ai/keld-cli/internal/config"
	"github.com/ncx-ai/keld-cli/internal/console"
	"github.com/ncx-ai/keld-cli/internal/errs"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate to Keld.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(Task 15): implement full device-flow login via RequireAuth.
			return errs.New("login flow not yet implemented (Task 15)")
		},
	}
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials.",
		RunE: func(cmd *cobra.Command, args []string) error {
			removed, err := auth.Clear()
			if err != nil {
				return err
			}
			if removed {
				console.Print("Logged out.")
			} else {
				console.Print("Not logged in.")
			}
			return nil
		},
	}
}

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the logged-in principal.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := auth.Load()
			if err != nil {
				return err
			}
			if a == nil {
				return console.Fail("not logged in (run `keld login`)")
			}
			line := fmt.Sprintf("%s · org %s · %s", a.Principal, a.Org, a.APIURL)
			m, err := config.LoadManifest()
			if err == nil && m.Endpoint != nil && *m.Endpoint != "" {
				line += fmt.Sprintf(" · endpoint %s", *m.Endpoint)
			}
			console.Print(line)
			return nil
		},
	}
}
