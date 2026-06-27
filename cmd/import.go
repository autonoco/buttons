package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/importer"
	"github.com/spf13/cobra"
)

var (
	importName string
	importYes  bool
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Create buttons from external sources (skill, code, url)",
	Long: `Import buttons from external sources:

  buttons import code <file>     wrap a script as a button (runtime inferred)
  buttons import skill <dir>     a button per script in an AgentSkills skill
  buttons import url <url>       create a button from a fetched HTTP spec
  buttons import mcp <server>    (planned) one button per MCP tool

Every import prints what it will create and asks to confirm. Pass --yes to
skip the prompt (required when running non-interactively).`,
}

var importCodeCmd = &cobra.Command{
	Use:   "code <file>",
	Short: "Wrap a script file as a button",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		plan, err := importer.PlanCode(args[0], importName)
		if err != nil {
			return importErr(err)
		}
		return runImport(plan)
	},
}

var importSkillCmd = &cobra.Command{
	Use:   "skill <dir>",
	Short: "Create buttons from an AgentSkills skill directory",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		plan, err := importer.PlanSkill(args[0])
		if err != nil {
			return importErr(err)
		}
		return runImport(plan)
	},
}

var importURLCmd = &cobra.Command{
	Use:   "url <url>",
	Short: "Create a button from a spec fetched over HTTP",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		plan, err := importer.PlanURL(args[0], importName)
		if err != nil {
			return importErr(err)
		}
		return runImport(plan)
	},
}

var importMCPCmd = &cobra.Command{
	Use:   "mcp <server>",
	Short: "Create buttons from an MCP server's tools (planned)",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return importErr(fmt.Errorf("mcp import is not implemented yet — it needs an MCP client to enumerate the server's tools (tracked in #277 follow-up). Use `buttons import code|skill|url` today"))
	},
}

// runImport shows the plan, confirms, and applies it.
func runImport(plan *importer.Plan) error {
	if len(plan.Items) == 0 {
		return importErr(fmt.Errorf("nothing to import"))
	}

	if !jsonOutput {
		fmt.Fprintf(os.Stderr, "Will create %d button(s) from %s:\n", len(plan.Items), plan.Kind)
		for _, it := range plan.Items {
			fmt.Fprintf(os.Stderr, "  • %-24s %-7s  %s\n", it.Name, it.Runtime, it.Source)
		}
	}

	if !importYes {
		if jsonOutput || config.IsNonTTY() {
			return importErr(fmt.Errorf("confirmation required: re-run with --yes to import non-interactively"))
		}
		fmt.Fprintf(os.Stderr, "Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(os.Stderr, "aborted")
			return nil
		}
	}

	res := importer.Apply(button.NewService(), plan)

	if jsonOutput {
		return config.WriteJSON(res)
	}
	for name, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", name, e)
	}
	fmt.Fprintf(os.Stderr, "Imported %d button(s): %s\n", len(res.Created), strings.Join(res.Created, ", "))
	if len(res.Created) > 0 {
		printNextHint("buttons press %s", res.Created[0])
	}
	return nil
}

func importErr(err error) error {
	if jsonOutput {
		_ = config.WriteJSONError("IMPORT_ERROR", err.Error())
		return errSilent
	}
	return err
}

func init() {
	importCmd.PersistentFlags().StringVar(&importName, "name", "", "override the generated button name (code/url)")
	importCmd.PersistentFlags().BoolVarP(&importYes, "yes", "y", false, "skip the confirmation prompt")
	importCmd.AddCommand(importCodeCmd, importSkillCmd, importURLCmd, importMCPCmd)
	rootCmd.AddCommand(importCmd)
}
