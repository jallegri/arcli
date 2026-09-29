// ingest subcommands administer runtime ingest settings on Arc.
package commands

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/basekick-labs/arcli/internal/client"
	"github.com/basekick-labs/arcli/internal/output"
)

func newIngestCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ingest",
		Short: "Inspect and change runtime ingest settings",
		Long: `Inspect and change process-wide Arc ingest settings.

Buffer settings require an admin token. In a cluster, arcli checks that every
node is healthy and reachable, then applies the setting to each node.`,
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
		persistentArg  string
	)
	c := &cobra.Command{
		Use:   "set",
		Short: "Set one or both ingest buffer limits (admin)",
		Long: `Change Arc's active ingest buffer limits and control whether the values persist across restarts.

	Omit a threshold flag to leave that setting unchanged. --persistent defaults
to true for compatibility. Set it to false to apply the thresholds only until
the next restart. In a cluster, the change is applied to every healthy node;
arcli preflights all nodes before changing any of them.
Both thresholds must be greater than zero.`,
		Example: `  arcli ingest buffer set --max-buffer-size 200000
  arcli ingest buffer set --max-buffer-age-ms 30000
  arcli ingest buffer set --max-buffer-size 200000 --max-buffer-age-ms 30000 --persistent true`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			persistent, err := strconv.ParseBool(persistentArg)
			if err != nil {
				return fmt.Errorf("--persistent must be true or false")
			}
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
				if !(cmd.Flags().Changed("persistent") && persistent) {
					return fmt.Errorf("nothing to update (pass a threshold or --persistent true)")
				}
			}
			patch.Persistent = &persistent
			cli, _, err := f.client(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := f.ctx(cmd)
			defer cancel()
			cfg, err := patchRuntimeIngestCluster(ctx, cli, patch)
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
	c.Flags().StringVar(&persistentArg, "persistent", "true", "persist setting across restarts: true or false")
	return c
}

func patchRuntimeIngestCluster(ctx context.Context, cli *client.Client, patch client.RuntimeIngestConfigPatch) (*client.RuntimeIngestConfig, error) {
	status, err := cli.ClusterStatus(ctx)
	if err != nil {
		var disabled *client.ClusterDisabledError
		if errors.As(err, &disabled) {
			return cli.PatchRuntimeIngestConfig(ctx, patch)
		}
		return nil, fmt.Errorf("inspect Arc cluster before changing ingest settings: %w", err)
	}
	if !status.Enabled || !status.Running {
		return nil, fmt.Errorf("Arc reports a cluster that is not running; refusing to change only part of its ingest configuration")
	}
	if status.NodeCount == 0 || len(status.Nodes) != status.NodeCount {
		return nil, fmt.Errorf("Arc cluster node inventory is incomplete (%d nodes reported, %d expected)", len(status.Nodes), status.NodeCount)
	}
	if status.HealthyCount != status.NodeCount {
		return nil, fmt.Errorf("all Arc cluster nodes must be healthy before changing ingest settings (%d/%d healthy)", status.HealthyCount, status.NodeCount)
	}

	nodes := make([]*client.Client, 0, len(status.Nodes))
	for _, node := range status.Nodes {
		if node.State != "healthy" {
			return nil, fmt.Errorf("Arc cluster node %q is %q; all nodes must be healthy", node.ID, node.State)
		}
		nodeClient := cli
		if node.ID != status.LocalNodeID {
			endpoint, endpointErr := clusterNodeEndpoint(cli, node.APIAddress)
			if endpointErr != nil {
				return nil, fmt.Errorf("build API endpoint for cluster node %q: %w", node.ID, endpointErr)
			}
			nodeClient, err = cli.ForEndpoint(endpoint)
			if err != nil {
				return nil, fmt.Errorf("create client for cluster node %q: %w", node.ID, err)
			}
		}
		if _, err := nodeClient.RuntimeIngestConfig(ctx); err != nil {
			return nil, fmt.Errorf("preflight cluster node %q failed; no settings were changed: %w", node.ID, err)
		}
		nodes = append(nodes, nodeClient)
	}

	previous := make([]*client.RuntimeIngestConfig, 0, len(nodes))
	for _, nodeClient := range nodes {
		old, err := nodeClient.RuntimeIngestConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("read prior ingest settings before cluster update: %w", err)
		}
		previous = append(previous, old)
	}

	var result *client.RuntimeIngestConfig
	for i, nodeClient := range nodes {
		result, err = nodeClient.PatchRuntimeIngestConfig(ctx, patch)
		if err != nil {
			rollbackErrors := make([]string, 0)
			// Include the node that returned an error: the request may have
			// committed before the client observed a transport failure.
			for j := i; j >= 0; j-- {
				old := previous[j]
				var rollbackErr error
				if old.Persistent || old.Source == "runtime_override" {
					oldSize, oldAge, oldPersistent := old.MaxBufferSize, old.MaxBufferAgeMS, old.Persistent
					_, rollbackErr = nodes[j].PatchRuntimeIngestConfig(ctx, client.RuntimeIngestConfigPatch{
						MaxBufferSize: &oldSize, MaxBufferAgeMS: &oldAge, Persistent: &oldPersistent,
					})
				} else {
					_, rollbackErr = nodes[j].ResetRuntimeIngestConfig(ctx)
				}
				if rollbackErr != nil {
					rollbackErrors = append(rollbackErrors, rollbackErr.Error())
				}
			}
			if len(rollbackErrors) > 0 {
				return nil, fmt.Errorf("cluster update failed on node %q (%w); rollback also failed: %s", status.Nodes[i].ID, err, strings.Join(rollbackErrors, "; "))
			}
			return nil, fmt.Errorf("cluster update failed on node %q (%w); prior node changes were rolled back", status.Nodes[i].ID, err)
		}
	}
	return result, nil
}

func clusterNodeEndpoint(cli *client.Client, address string) (string, error) {
	base, err := url.Parse(cli.Endpoint())
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("invalid configured Arc endpoint")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid API address %q: %w", address, err)
	}
	if host == "" {
		host = base.Hostname()
	}
	if port == "" {
		return "", fmt.Errorf("API address %q has no port", address)
	}
	base.Host = net.JoinHostPort(host, port)
	base.Path = ""
	base.RawQuery = ""
	base.Fragment = ""
	return strings.TrimRight(base.String(), "/"), nil
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
