package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/internal/output"
)

var capabilitiesCmd = &cobra.Command{
	Use:   "capabilities",
	Short: "Print this ghost's supported features and trailer schema as JSON",
	Long: `Print a JSON capability descriptor so core can probe a base image's ghost
version at executor creation and decide whether to use supervise mode (with a
result trailer) or fall back to the legacy exec path against an older image.`,
	Example: `  ghost capabilities`,
	RunE:    capabilitiesCommand,
}

func capabilitiesCommand(cmd *cobra.Command, args []string) error {
	data, err := json.Marshal(output.Current())
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
