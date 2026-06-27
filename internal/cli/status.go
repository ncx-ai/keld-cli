package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ncx-ai/keld-cli/internal/auth"
	"github.com/ncx-ai/keld-cli/internal/config"
	"github.com/ncx-ai/keld-cli/internal/console"
	"github.com/ncx-ai/keld-cli/internal/tools"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Keld Signal configuration status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := auth.Load()
			if err != nil {
				return err
			}
			if a == nil {
				console.Print("Not logged in (run `keld login`)")
			} else {
				console.Print(fmt.Sprintf("Logged in: %s · org %s · %s", a.Principal, a.Org, a.APIURL))
			}

			manifest, err := config.LoadManifest()
			if err != nil {
				return err
			}

			for _, adapter := range tools.All() {
				tm, inManifest := manifest.Tools[adapter.Name()]
				var st tools.ToolStatus
				if inManifest {
					var current *string
					if data, err := os.ReadFile(tm.ConfigPath); err == nil {
						s := string(data)
						current = &s
					}
					st = adapter.Status(current, tm.Managed)
				} else {
					st = adapter.Status(nil, nil)
				}

				var state string
				switch {
				case st.Configured:
					state = "configured"
				case st.Installed:
					state = "not configured"
				default:
					state = "not installed"
				}
				console.Print(fmt.Sprintf("  %-14s %s", adapter.DisplayName(), state))
			}

			if manifest.Hook != nil {
				console.Print(fmt.Sprintf("  hook            v%s", manifest.Hook.Version))
			}

			return nil
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Keld Signal configuration for problems.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var problems []string

			manifest, err := config.LoadManifest()
			if err != nil {
				return err
			}

			for name, tm := range manifest.Tools {
				adapter, err := tools.Get(name)
				if err != nil {
					// Unknown tool in manifest — skip silently (matches Python behaviour).
					continue
				}
				var current *string
				if data, err := os.ReadFile(tm.ConfigPath); err == nil {
					s := string(data)
					current = &s
				}
				st := adapter.Status(current, tm.Managed)
				if !st.Configured {
					problems = append(problems,
						fmt.Sprintf("%s: manifest records setup but config is not configured (drift). Re-run `keld setup`.", adapter.DisplayName()),
					)
				}
			}

			// TODO(Task 18): check hook.json presence; for Phase 1, skip file-exists
			// check if path is empty.
			if manifest.Hook != nil && manifest.Hook.Path != "" {
				if _, err := os.Stat(manifest.Hook.Path); os.IsNotExist(err) {
					problems = append(problems, "hook script is missing. Re-run `keld setup`.")
				}
			}

			if len(problems) > 0 {
				for _, p := range problems {
					console.Print(fmt.Sprintf("  ✗ %s", p))
				}
				return fmt.Errorf("problems found")
			}
			console.Print("No problems found.")
			return nil
		},
	}
}
