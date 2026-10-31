package policy

import (
	"context"
	"encoding/json"
	"fmt"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

type getInput struct {
	Revision uint64
}

// getResult is the policy settings document. The table and plain forms are
// the YAML that `safedep policy edit` opens, so `safedep policy get >
// policy.yaml` writes a file that a person or a script can read.
type getResult struct {
	Revision uint64
	Document any
	YAML     string
}

func getCmd(a *app.App) *cobra.Command {
	var in getInput
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Write the policy settings document as YAML",
		Long: "Write the policy settings document of the tenant as YAML: the policy base and every policy " +
			"override. Package rules are not in it. By default the command writes the head revision. " +
			"Use --revision for an older one. With --output json, the command writes the same document " +
			"as JSON.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runGet(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), in, a.Tenant())
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	cmd.Flags().Uint64Var(&in.Revision, "revision", 0, "the policy revision to write; the head revision when 0")
	return cmd
}

func runGet(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	in getInput,
	tenant string,
) (*getResult, error) {
	revision, policy, err := readPolicyAt(ctx, client, in.Revision)
	if err != nil {
		return nil, err
	}
	doc, err := cloudpolicy.PolicyMap(policy)
	if err != nil {
		return nil, err
	}
	text, err := cloudpolicy.MarshalPolicy(policy, getHeader(tenant, revision))
	if err != nil {
		return nil, err
	}
	return &getResult{Revision: revision, Document: doc, YAML: string(trimNewline(text))}, nil
}

// readPolicyAt reads the policy settings at a revision, or at head for 0.
func readPolicyAt(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	revision uint64,
) (uint64, *policyv1.Policy, error) {
	if revision == 0 {
		res, err := getPolicy(ctx, client)
		if err != nil {
			return 0, nil, err
		}
		return res.GetHeadRevision(), res.GetPolicy(), nil
	}
	req := &policyv1.GetPolicyRevisionRequest{}
	req.SetRevision(revision)
	res, err := client.GetPolicyRevision(ctx, req)
	if err != nil {
		return 0, nil, fmt.Errorf("policy: get revision %d: %w", revision, err)
	}
	return revision, res.GetPolicy(), nil
}

func getHeader(tenant string, revision uint64) string {
	if tenant == "" {
		tenant = "this tenant"
	}
	return cloudpolicy.CommentBlock([]string{
		fmt.Sprintf("Policy settings of %s%srevision %d", tenant, sep, revision),
		"Package rules are not in this file: safedep policy rule list",
	})
}

func (r *getResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(struct {
		Revision uint64 `json:"revision"`
		Policy   any    `json:"policy"`
	}{Revision: r.Revision, Policy: r.Document}, "", "  ")
}

// RenderTable escapes the document for a terminal. yaml.v3 escapes control
// characters in strings, but not bidirectional format characters.
func (r *getResult) RenderTable() string { return clitui.EscapeText(r.YAML) }

// RenderPlain writes the document as it is, because `policy get > file`
// must give back the exact values.
func (r *getResult) RenderPlain() string { return r.YAML }

func trimNewline(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b
}
