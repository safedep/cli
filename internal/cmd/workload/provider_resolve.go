package workload

import (
	"context"
	"fmt"
	"strings"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"

	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

// listAllProviders reads every page of the providers of the tenant. A tenant
// has one provider for each linked account, so the list is short.
func listAllProviders(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	command string,
) ([]*controltowerv1.WorkloadIdentityProvider, error) {
	var providers []*controltowerv1.WorkloadIdentityProvider
	err := paging.Paginate(ctx, "workload "+command, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListWorkloadIdentityProvidersRequest{}
		req.SetPagination(paging.NewPaginationRequest(0, pageToken))
		res, err := client.ListWorkloadIdentityProviders(ctx, req)
		if err != nil {
			return "", mapError(command, err)
		}
		providers = append(providers, res.GetProviders()...)
		return res.GetPagination().GetNextPageToken(), nil
	})
	return providers, err
}

// providerNames maps each provider id of the tenant to its KIND:ACCOUNT name.
func providerNames(providers []*controltowerv1.WorkloadIdentityProvider) map[string]string {
	names := make(map[string]string, len(providers))
	for _, p := range providers {
		names[p.GetProviderId()] = providerName(p)
	}
	return names
}

// resolveProvider finds the provider that ref names: a provider id, or a
// KIND:ACCOUNT name, as in github-actions:acme. The account matches with no
// regard to case, as a GitHub login does. The CLI never picks a provider on
// its own, even when the tenant has one provider: a rule then names its
// provider in every script that creates it.
func resolveProvider(
	providers []*controltowerv1.WorkloadIdentityProvider,
	ref string,
) (*controltowerv1.WorkloadIdentityProvider, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, providerError("--provider is required. The CLI never picks a provider", providers)
	}
	for _, p := range providers {
		if p.GetProviderId() == ref {
			return p, nil
		}
	}
	var matches []*controltowerv1.WorkloadIdentityProvider
	for _, p := range providers {
		if strings.EqualFold(providerName(p), ref) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, providerError(fmt.Sprintf("--provider %s is not a provider of the tenant", tui.EscapeLine(ref)), providers)
	}
	return nil, providerError(fmt.Sprintf("--provider %s names more than one provider. Pass the provider id",
		tui.EscapeLine(ref)), matches)
}

// providerError lists the providers that --provider can take, so the user
// can run the command again with one of them.
func providerError(problem string, providers []*controltowerv1.WorkloadIdentityProvider) error {
	if len(providers) == 0 {
		return invalidInputError(problem+". The tenant has no workload identity provider", noProvidersHelp)
	}
	lines := []string{problem + ". The providers of the tenant:"}
	for _, p := range providers {
		lines = append(lines, fmt.Sprintf("  %s  id %s, link %s", tui.EscapeLine(providerName(p)),
			tui.EscapeLine(p.GetProviderId()), tui.EnumToken(p.GetLinkState().String(), linkStatePrefix)))
	}
	return invalidInputError(strings.Join(lines, "\n"),
		"Run the command again with --provider "+tui.EscapeLine(providerName(providers[0])))
}
