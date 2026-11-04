package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	auditv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/audit/v1/auditv1grpc"
	auditv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/audit/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/duration"
	"github.com/safedep/cli/internal/paging"
)

const resultPrefix = "AUDIT_RESULT_"

// eventLister is the one RPC that the command calls.
type eventLister interface {
	ListAuditEvents(
		context.Context,
		*auditv1.ListAuditEventsRequest,
		...grpc.CallOption,
	) (*auditv1.ListAuditEventsResponse, error)
}

type listInput struct {
	Since     time.Duration
	Until     time.Duration
	Action    string
	Resource  string
	Actor     string
	PageSize  uint32
	PageToken string
}

func eventListCmd(a *app.App) *cobra.Command {
	var in listInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List audit events, newest first",
		Long: "List the audit events of the tenant, newest first. --since and --until take a duration " +
			"before now, as in 7d or 12h. --action takes a prefix of the action: --action policy. lists " +
			"every policy action, and --action policy.rule. lists the package rule actions. --resource " +
			"takes a resource kind, as in policy_change, package_rule, endpoint or project, or a kind and " +
			"an id, as in policy_change/18. --actor matches the actor id or the display name exactly. " +
			"The command reads one page. Pass --page-token to read the next page.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			_, err := newListRequest(in, time.Now())
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runList(cmd.Context(), auditv1grpc.NewAuditServiceClient(client.Connection()), in, time.Now())
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.Var(duration.NewValue(&in.Since, 0), "since", "only events in this time before now, as in 7d or 12h")
	f.Var(duration.NewValue(&in.Until, 0), "until", "only events older than this time before now, as in 1d")
	f.StringVar(&in.Action, "action", "", "only events whose action starts with this text; policy. lists the policy actions")
	f.StringVar(&in.Resource, "resource", "",
		"only events about a resource: KIND (policy_change, package_rule, endpoint, project) or KIND/ID, as in policy_change/18")
	f.StringVar(&in.Actor, "actor", "", "only events of this actor id or display name, as in alice@acme.io")
	f.Uint32Var(&in.PageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func invalidFilter(cause error, help string) error {
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError(cause.Error()).
		WithHelp(help).
		Wrap(cause)
}

func newListRequest(in listInput, now time.Time) (*auditv1.ListAuditEventsRequest, error) {
	filters := &auditv1.ListAuditEventsRequest_Filters{}
	set := false
	after, before, err := duration.Window(in.Since, in.Until, now, "events")
	if err != nil {
		return nil, err
	}
	if after != nil {
		filters.SetOccurredAfter(timestamppb.New(*after))
		set = true
	}
	if before != nil {
		filters.SetOccurredBefore(timestamppb.New(*before))
		set = true
	}
	if in.Action != "" {
		filters.SetActionPrefix(in.Action)
		set = true
	}
	if in.Resource != "" {
		kind, id, hasID := strings.Cut(in.Resource, "/")
		if kind == "" || (hasID && id == "") {
			return nil, invalidFilter(fmt.Errorf("--resource %q: write KIND or KIND/ID", in.Resource),
				"As in --resource package_rule or --resource policy_change/18")
		}
		filters.SetResourceKind(kind)
		if hasID {
			filters.SetResourceId(id)
		}
		set = true
	}
	if in.Actor != "" {
		filters.SetActor(in.Actor)
		set = true
	}

	req := &auditv1.ListAuditEventsRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	if set {
		req.SetFilters(filters)
	}
	return req, nil
}

func runList(ctx context.Context, client eventLister, in listInput, now time.Time) (*listResult, error) {
	req, err := newListRequest(in, now)
	if err != nil {
		return nil, err
	}
	res, err := client.ListAuditEvents(ctx, req)
	if err != nil {
		return nil, mapListError(err)
	}
	out := &listResult{
		Events:        make([]eventView, 0, len(res.GetEvents())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, e := range res.GetEvents() {
		out.Events = append(out.Events, newEventView(e))
	}
	return out, nil
}

func mapListError(err error) error {
	cause := fmt.Errorf("audit event list: %w", err)
	if st, ok := status.FromError(err); ok && st.Code() == codes.PermissionDenied {
		return usefulerror.NewUsefulError().
			WithCode(usefulerror.ErrAuthorizationFailed).
			WithHumanError("Only an Admin can read the audit log").
			WithHelp("Ask an Admin of the tenant to run the command").
			Wrap(cause)
	}
	return cause
}
