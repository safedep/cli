package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

type revisionShowResult struct {
	Revision *revisionView
	Document any
	YAML     string
}

func revisionShowCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show REVISION",
		Short: "Show one policy revision with its policy settings",
		Long: "Show one policy revision: who published it, when, from which change, its change direction, " +
			"and the policy settings document at that revision.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			revision, err := parseRevision(args[0])
			if err != nil {
				return err
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runRevisionShow(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), revision)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
}

func parseRevision(text string) (uint64, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(text, "rev"), 10, 64)
	if err != nil || n == 0 {
		return 0, invalidInputError(fmt.Errorf("revision %q is not a revision number", text),
			"Give a number from: safedep policy revision list")
	}
	return n, nil
}

func runRevisionShow(ctx context.Context, client policyv1grpc.PolicyServiceClient, revision uint64) (*revisionShowResult, error) {
	req := &policyv1.GetPolicyRevisionRequest{}
	req.SetRevision(revision)
	res, err := client.GetPolicyRevision(ctx, req)
	if err != nil {
		return nil, mapRevisionError("policy revision show", revision, err)
	}
	doc, err := cloudpolicy.PolicyMap(res.GetPolicy())
	if err != nil {
		return nil, err
	}
	text, err := cloudpolicy.MarshalPolicy(res.GetPolicy(), "")
	if err != nil {
		return nil, err
	}
	view := newRevisionView(res.GetRevision())
	if view == nil {
		return nil, fmt.Errorf("policy revision show: invalid response: revision %d is missing", revision)
	}
	return &revisionShowResult{Revision: view, Document: doc, YAML: string(trimNewline(text))}, nil
}

func (r *revisionShowResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(struct {
		Revision *revisionView `json:"revision"`
		Policy   any           `json:"policy"`
	}{Revision: r.Revision, Policy: r.Document}, "", "  ")
}

func (r *revisionShowResult) RenderTable() string {
	rev := r.Revision
	change := "-"
	if rev.ChangeID != "" {
		change = rev.ChangeID + sep + "safedep policy change show " + rev.ChangeID
	}
	rows := [][2]string{
		{"Revision", rev.summary(time.Now())},
		{"Source", rev.Source},
		{"Direction", rev.Direction},
		{"Change", change},
	}
	return cloudpolicy.KeyValues(rows) + "\n\nPolicy settings\n" + clitui.EscapeText(r.YAML)
}

func (r *revisionShowResult) RenderPlain() string {
	rev := r.Revision
	return strings.Join([]string{
		clitui.PlainRow("revision", fmt.Sprint(rev.Revision)),
		clitui.PlainRow("published_at", rev.Published),
		clitui.PlainRow("author", rev.Author),
		clitui.PlainRow("source", rev.Source),
		clitui.PlainRow("direction", rev.Direction),
		clitui.PlainRow("change_id", rev.ChangeID),
		clitui.PlainRow("message", rev.Message),
		clitui.EscapeText(r.YAML),
	}, "\n")
}
