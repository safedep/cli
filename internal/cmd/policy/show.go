package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

type showResult struct {
	Tenant          string
	HeadRevision    uint64
	Head            *revisionView
	DefaultRevision uint64
	DefaultFields   []cloudpolicy.FieldValue
	OpenChanges     uint32
	TrustRules      uint32
	BlockRules      uint32
	ExpiringSoon    uint32
	BaseDescription string
	Overrides       []overrideView
	Document        any
}

type showResultJSON struct {
	Tenant          string            `json:"tenant,omitempty"`
	HeadRevision    uint64            `json:"head_revision"`
	Head            *revisionView     `json:"head,omitempty"`
	DefaultRevision uint64            `json:"default_policy_revision"`
	DefaultPolicy   map[string]string `json:"default_policy"`
	OpenChanges     uint32            `json:"open_change_count"`
	PackageRules    map[string]uint32 `json:"package_rule_counts"`
	Overrides       []overrideView    `json:"overrides"`
	Policy          any               `json:"policy"`
}

func showCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show a summary of the tenant policy",
		Long: "Show the head policy revision, the open policy changes, the package rule counts, the " +
			"SafeDep default policy and the policy overrides in priority order. With --output json, the " +
			"output also holds the policy settings document.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runShow(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), a.Tenant())
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
}

func runShow(ctx context.Context, client policyv1grpc.PolicyServiceClient, tenant string) (*showResult, error) {
	res, err := getPolicy(ctx, client)
	if err != nil {
		return nil, err
	}
	doc, err := cloudpolicy.PolicyMap(res.GetPolicy())
	if err != nil {
		return nil, err
	}
	counts := res.GetPackageRuleCounts()
	return &showResult{
		Tenant:          tenant,
		HeadRevision:    res.GetHeadRevision(),
		Head:            newRevisionView(res.GetHead()),
		DefaultRevision: res.GetDefaultPolicy().GetRevision(),
		DefaultFields:   cloudpolicy.Fields(res.GetDefaultPolicy().GetSpec()),
		OpenChanges:     res.GetOpenChangeCount(),
		TrustRules:      counts.GetTrustCount(),
		BlockRules:      counts.GetBlockCount(),
		ExpiringSoon:    counts.GetTrustExpiringSoonCount(),
		BaseDescription: res.GetPolicy().GetBaseDescription(),
		Overrides:       overrideViews(res.GetPolicy()),
		Document:        doc,
	}, nil
}

func (r *showResult) RenderJSON() ([]byte, error) {
	defaults := make(map[string]string, len(r.DefaultFields))
	for _, f := range r.DefaultFields {
		defaults[f.Path] = f.Value
	}
	overrides := r.Overrides
	if overrides == nil {
		overrides = []overrideView{}
	}
	return json.MarshalIndent(showResultJSON{
		Tenant:          r.Tenant,
		HeadRevision:    r.HeadRevision,
		Head:            r.Head,
		DefaultRevision: r.DefaultRevision,
		DefaultPolicy:   defaults,
		OpenChanges:     r.OpenChanges,
		PackageRules: map[string]uint32{
			"trust":                r.TrustRules,
			"block":                r.BlockRules,
			"trust_expiring_in_7d": r.ExpiringSoon,
			"total":                r.TrustRules + r.BlockRules,
		},
		Overrides: overrides,
		Policy:    r.Document,
	}, "", "  ")
}

func (r *showResult) headLine(now time.Time) string {
	if r.Head == nil {
		return "none: the tenant has not published a policy yet"
	}
	return r.Head.summary(now)
}

func (r *showResult) openLine() string {
	if r.OpenChanges == 0 {
		return "none"
	}
	return fmt.Sprintf("%d%ssafedep policy change list --state open", r.OpenChanges, sep)
}

func (r *showResult) rulesLine() string {
	line := fmt.Sprintf("%d (%d trust, %d block)", r.TrustRules+r.BlockRules, r.TrustRules, r.BlockRules)
	if r.ExpiringSoon > 0 {
		line += fmt.Sprintf("%s%d expire in 7d", sep, r.ExpiringSoon)
	}
	return line
}

func (r *showResult) defaultLine() string {
	parts := []string{fmt.Sprintf("revision %d", r.DefaultRevision)}
	for _, f := range r.DefaultFields {
		parts = append(parts, f.Path+": "+f.Human)
	}
	return strings.Join(parts, sep)
}

func (r *showResult) RenderTable() string {
	now := time.Now()
	tenant := r.Tenant
	if tenant == "" {
		tenant = "(this tenant)"
	}
	rows := [][2]string{
		{"Policy", tenant},
		{"Head revision", r.headLine(now)},
		{"Open changes", r.openLine()},
		{"Package rules", r.rulesLine()},
		{"Default policy", r.defaultLine()},
	}
	if r.BaseDescription != "" {
		rows = append(rows, [2]string{"Base", r.BaseDescription})
	}

	overrideRows := make([][]string, 0, len(r.Overrides))
	for _, o := range r.Overrides {
		expires := "-"
		if o.expires != nil {
			expires = humanTimeCell(o.expires, now)
		}
		overrideRows = append(overrideRows, []string{
			fmt.Sprintf("%d", o.Priority), o.Name, o.Selector, expires, cloudpolicy.EmptyDash(o.Description),
		})
	}
	overrides := table.New().
		Title("Overrides (priority, highest first)").
		Headers("PRIORITY", "NAME", "SELECTOR", "EXPIRES", "DESCRIPTION").
		Rows(clitui.EscapeCells(overrideRows)...).
		EmptyMessage("No policy overrides. The policy base applies to every subject. Add one with: safedep policy edit").
		Render()
	return cloudpolicy.KeyValues(rows) + "\n\n" + overrides
}

func (r *showResult) RenderPlain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "tenant\t%s\n", clitui.EscapeLine(r.Tenant))
	fmt.Fprintf(&b, "head_revision\t%d\n", r.HeadRevision)
	fmt.Fprintf(&b, "default_policy_revision\t%d\n", r.DefaultRevision)
	fmt.Fprintf(&b, "open_change_count\t%d\n", r.OpenChanges)
	fmt.Fprintf(&b, "trust_rules\t%d\n", r.TrustRules)
	fmt.Fprintf(&b, "block_rules\t%d\n", r.BlockRules)
	fmt.Fprintf(&b, "trust_expiring_in_7d\t%d", r.ExpiringSoon)
	for _, o := range r.Overrides {
		b.WriteString("\n" + clitui.PlainRow("override", fmt.Sprint(o.Priority), o.Name, o.Selector, o.ExpiresAt))
	}
	return b.String()
}
