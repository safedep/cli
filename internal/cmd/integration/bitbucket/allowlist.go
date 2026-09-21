package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/tui"
)

// UpdateBitbucketRepositoryAllowlist caps enable and disable at 500 UUIDs
// per request. A larger allowlist grows over several calls.
const maxAllowlistItems = 500

type allowlistUpdateInput struct {
	LinkID  string
	Scope   string
	Enable  []string
	Disable []string
}

type allowlistUpdateResult struct {
	linkID         string
	scanScope      string
	allowlistCount uint32
}

type allowlistUpdateResultJSON struct {
	LinkID                     string `json:"link_id"`
	ScanScope                  string `json:"scan_scope"`
	AllowlistedRepositoryCount uint32 `json:"allowlisted_repository_count"`
}

func allowlistUpdateCmd(a *app.App) *cobra.Command {
	var in allowlistUpdateInput
	cmd := &cobra.Command{
		Use:   "update",
		Args:  cobra.NoArgs,
		Short: "Update the Bitbucket scan scope and repository allowlist",
		Long: "Change which repositories of a linked Bitbucket workspace SafeDep scans. " +
			"Scope `all` scans every repository in the workspace. Scope `selected` scans only " +
			"the repositories on the allowlist, which --enable and --disable edit by repository " +
			"UUID. A fresh link starts with scope `selected` and an empty allowlist, so nothing " +
			"is scanned until this command opens the scope or fills the allowlist.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			integration := newIntegrationClient(client.Connection())
			result, err := runAllowlistUpdate(cmd.Context(), integration, integration, in)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}

	f := cmd.Flags()
	f.StringVar(&in.LinkID, "link-id", "",
		"Bitbucket workspace link to update; resolved automatically when the tenant has exactly one link")
	f.StringVar(&in.Scope, "scope", "",
		"scan scope after the update: all or selected (omit to keep the stored scope)")
	f.StringArrayVar(&in.Enable, "enable", nil,
		"repository UUID to add to the scan allowlist; repeat for multiple repositories")
	f.StringArrayVar(&in.Disable, "disable", nil,
		"repository UUID to remove from the scan allowlist; repeat for multiple repositories")
	return cmd
}

func runAllowlistUpdate(
	ctx context.Context,
	links linkLister,
	updater allowlistUpdater,
	in allowlistUpdateInput,
) (*allowlistUpdateResult, error) {
	const label = "integration bitbucket allowlist update"

	scope, err := parseScanScope(in)
	if err != nil {
		return nil, err
	}
	if err := validateAllowlistSelection(in); err != nil {
		return nil, err
	}

	linkID := in.LinkID
	if linkID == "" {
		resolved, err := resolveLinkID(ctx, links, label)
		if err != nil {
			return nil, err
		}
		linkID = resolved
	}

	req := &controltowerv1.UpdateBitbucketRepositoryAllowlistRequest{}
	req.SetLinkId(linkID)
	req.SetScanScope(scope)
	req.SetEnable(in.Enable)
	req.SetDisable(in.Disable)

	res, err := updater.UpdateBitbucketRepositoryAllowlist(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return &allowlistUpdateResult{
		linkID:         linkID,
		scanScope:      tui.EnumToken(res.GetScanScope().String(), scanScopePrefix),
		allowlistCount: res.GetAllowlistedRepositoryCount(),
	}, nil
}

// parseScanScope maps the --scope token to the proto enum. An empty flag
// keeps the stored scope: the server treats UNSPECIFIED as no change.
func parseScanScope(in allowlistUpdateInput) (controltowerv1.BitbucketScanScope, error) {
	if in.Scope == "" {
		return controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_UNSPECIFIED, nil
	}
	number, ok := tui.ParseEnumToken(controltowerv1.BitbucketScanScope_name, scanScopePrefix, in.Scope)
	if !ok {
		allowed := strings.Join(tui.EnumTokens(controltowerv1.BitbucketScanScope_name, scanScopePrefix), ", ")
		cause := fmt.Errorf("unknown --scope value %q: allowed values are %s", in.Scope, allowed)
		return 0, invalidSelectionError(cause, fmt.Sprintf("Retry --scope with one of: %s.", allowed))
	}
	scope := controltowerv1.BitbucketScanScope(number)
	if scope == controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_ALL &&
		(len(in.Enable) > 0 || len(in.Disable) > 0) {
		cause := fmt.Errorf("scope all takes no --enable or --disable values")
		return 0, invalidSelectionError(
			cause,
			"Scope all scans every repository, so drop --enable and --disable, or use --scope selected.",
		)
	}
	return scope, nil
}

func validateAllowlistSelection(in allowlistUpdateInput) error {
	if in.Scope == "" && len(in.Enable) == 0 && len(in.Disable) == 0 {
		cause := fmt.Errorf("nothing to update")
		return invalidSelectionError(cause, "Pass --scope, --enable, or --disable.")
	}
	if len(in.Enable) > maxAllowlistItems || len(in.Disable) > maxAllowlistItems {
		cause := fmt.Errorf("at most %d repository UUIDs per flag per request", maxAllowlistItems)
		return invalidSelectionError(
			cause,
			fmt.Sprintf("Pass at most %d --enable and %d --disable values and repeat the command for the rest.",
				maxAllowlistItems, maxAllowlistItems),
		)
	}
	if err := validateUniqueUUIDs(in.Enable, "enable"); err != nil {
		return err
	}
	if err := validateUniqueUUIDs(in.Disable, "disable"); err != nil {
		return err
	}
	disable := make(map[string]struct{}, len(in.Disable))
	for _, uuid := range in.Disable {
		disable[uuid] = struct{}{}
	}
	for _, uuid := range in.Enable {
		if _, both := disable[uuid]; both {
			cause := fmt.Errorf("repository UUID %q is in both --enable and --disable", uuid)
			return invalidSelectionError(
				cause,
				fmt.Sprintf("Remove repository UUID %q from one of the flags and retry.", uuid),
			)
		}
	}
	return nil
}

func validateUniqueUUIDs(values []string, flag string) error {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		if value == "" {
			cause := fmt.Errorf("--%s value at position %d must not be empty", flag, i+1)
			return invalidSelectionError(
				cause,
				fmt.Sprintf("Provide a repository UUID at --%s position %d.", flag, i+1),
			)
		}
		if _, duplicate := seen[value]; duplicate {
			cause := fmt.Errorf("duplicate --%s value %q", flag, value)
			return invalidSelectionError(
				cause,
				fmt.Sprintf("Remove duplicate --%s value %q and retry.", flag, value),
			)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (r *allowlistUpdateResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(allowlistUpdateResultJSON{
		LinkID:                     r.linkID,
		ScanScope:                  r.scanScope,
		AllowlistedRepositoryCount: r.allowlistCount,
	}, "", "  ")
}

func (r *allowlistUpdateResult) RenderPlain() string {
	return "link_id\tscan_scope\tallowlisted_repository_count\n" + strings.Join(r.cells(), "\t")
}

func (r *allowlistUpdateResult) RenderTable() string {
	return table.New().
		Title("Bitbucket scan allowlist").
		Headers("LINK ID", "SCAN SCOPE", "ALLOWLISTED REPOSITORIES").
		Rows(r.cells()).
		Render()
}

func (r *allowlistUpdateResult) cells() []string {
	return []string{
		r.linkID,
		r.scanScope,
		fmt.Sprintf("%d", r.allowlistCount),
	}
}
