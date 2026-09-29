// ingest subcommands administer runtime ingest settings on Arc.
package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/basekick-labs/arcli/internal/client"
	"github.com/basekick-labs/arcli/internal/output"
)

func newIngestCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ingest",
		Short: "Inspect and change runtime ingest settings",
		Long: `Inspect and change process-wide Arc ingest settings.

Buffer settings are persisted by Arc and require an admin token. The current
settings apply to this Arc process; in a multi-node deployment, configure each
node separately.`,
	}
	c.AddCommand(newIngestBufferCmd())
	return c
}

func newIngestBufferCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "buffer",
		Short: "Inspect and change ingest buffer limits",
	}
	c.AddCommand(newIngestBufferShowCmd(), newIngestBufferSetCmd(), newIngestBufferResetCmd())
	return c
}

func newIngestBufferShowCmd() *cobra.Command {
	var (
		f            connFlags
		outputFormat string
	)
	c := &cobra.Command{
		Use:   "show",
		Short: "Show the active ingest buffer limits (admin)",
		Long:  "Show Arc's effective runtime ingest buffer settings (GET /api/v1/config/runtime/ingest).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validObjectFormat(outputFormat) {
				return fmt.Errorf("invalid --output %q (valid: table, json)", outputFormat)
			}
			cli, _, err := f.client(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := f.ctx(cmd)
			defer cancel()
			cfg, err := cli.RuntimeIngestConfig(ctx)
			if err != nil {
				return err
			}
			return writeRuntimeIngestConfig(cmd, outputFormat, "", cfg)
		},
	}
	f.add(c)
	c.Flags().StringVarP(&outputFormat, "output", "o", output.FormatTable, "output format: table|json")
	return c
}

func newIngestBufferSetCmd() *cobra.Command {
	var (
		f              connFlags
		outputFormat   string
		maxBufferSize  int
		maxBufferAgeMS int
	)
	c := &cobra.Command{
		Use:   "set",
		Short: "Set one or both ingest buffer limits (admin)",
		Long: `Change Arc's active ingest buffer limits and persist the override.

Omit a flag to leave that setting unchanged. Arc applies the settings to the
current process immediately and restores the saved values after a restart.
Both values must be greater than zero.`,
		Example: `  arcli ingest buffer set --max-buffer-size 200000
  arcli ingest buffer set --max-buffer-age-ms 30000
  arcli ingest buffer set --max-buffer-size 200000 --max-buffer-age-ms 30000`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validObjectFormat(outputFormat) {
				return fmt.Errorf("invalid --output %q (valid: table, json)", outputFormat)
			}
			flags := cmd.Flags()
			patch := client.RuntimeIngestConfigPatch{}
			if flags.Changed("max-buffer-size") {
				if maxBufferSize <= 0 {
					return fmt.Errorf("--max-buffer-size must be greater than zero")
				}
				patch.MaxBufferSize = &maxBufferSize
			}
			if flags.Changed("max-buffer-age-ms") {
				if maxBufferAgeMS <= 0 {
					return fmt.Errorf("--max-buffer-age-ms must be greater than zero")
				}
				patch.MaxBufferAgeMS = &maxBufferAgeMS
			}
			if patch.MaxBufferSize == nil && patch.MaxBufferAgeMS == nil {
				return fmt.Errorf("nothing to update (pass --max-buffer-size and/or --max-buffer-age-ms)")
			}
			cli, _, err := f.client(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := f.ctx(cmd)
			defer cancel()
			cfg, err := cli.PatchRuntimeIngestConfig(ctx, patch)
			if err != nil {
				return err
			}
			return writeRuntimeIngestConfig(cmd, outputFormat, "Updated", cfg)
		},
	}
	f.add(c)
	c.Flags().StringVarP(&outputFormat, "output", "o", output.FormatTable, "output format: table|json")
	c.Flags().IntVar(&maxBufferSize, "max-buffer-size", 0, "maximum records buffered before flushing (>0)")
	c.Flags().IntVar(&maxBufferAgeMS, "max-buffer-age-ms", 0, "maximum buffer age in milliseconds before flushing (>0)")
	return c
}

func newIngestBufferResetCmd() *cobra.Command {
	var (
		f            connFlags
		outputFormat string
	)
	c := &cobra.Command{
		Use:   "reset",
		Short: "Restore startup ingest buffer limits (admin)",
		Long:  "Remove Arc's persisted ingest buffer override and restore startup configuration (DELETE /api/v1/config/runtime/ingest).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validObjectFormat(outputFormat) {
				return fmt.Errorf("invalid --output %q (valid: table, json)", outputFormat)
			}
			cli, _, err := f.client(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := f.ctx(cmd)
			defer cancel()
			cfg, err := cli.ResetRuntimeIngestConfig(ctx)
			if err != nil {
				return err
			}
			return writeRuntimeIngestConfig(cmd, outputFormat, "Restored startup", cfg)
		},
	}
	f.add(c)
	c.Flags().StringVarP(&outputFormat, "output", "o", output.FormatTable, "output format: table|json")
	return c
}

func writeRuntimeIngestConfig(cmd *cobra.Command, outputFormat, action string, cfg *client.RuntimeIngestConfig) error {
	w := cmd.OutOrStdout()
	if outputFormat == output.FormatJSON {
		return writeRawJSON(w, cfg.Raw)
	}
	if action != "" {
		if _, err := fmt.Fprintln(w, action+" runtime ingest buffer settings:"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "max_buffer_size:  %d\n", cfg.MaxBufferSize); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "max_buffer_age_ms: %d\n", cfg.MaxBufferAgeMS); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "scope:             %s\n", clean(cfg.Scope)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "persistent:        %t\n", cfg.Persistent); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "source:            %s\n", clean(cfg.Source))
	return err
}
