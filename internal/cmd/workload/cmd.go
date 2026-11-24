// Package workload registers the `safedep workload` command tree. It manages
// workload identity in SafeDep Cloud: the workload identity providers of the
// tenant, the workload trust rules that let CI jobs of a provider call
// SafeDep with no long-lived key, and the record of each workload exchange.
// Every verb needs control plane credentials and the Admin role.
package workload

import (
	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "workload",
		Short: "Manage workload identity for CI jobs",
		Long: "Manage workload identity in your SafeDep Cloud tenant. A CI job, such as a GitHub Actions job, " +
			"trades the identity token of its platform for a short-lived SafeDep credential, with no API key " +
			"in the CI secrets. A workload identity provider trusts the tokens of one platform account. A " +
			"workload trust rule says which jobs of a provider get a credential. Every command needs the Admin role.",
	}

	provider := &cobra.Command{
		Use:   "provider",
		Short: "Inspect workload identity providers",
		Long: "Inspect the workload identity providers of the tenant. A verified GitHub App link creates the " +
			"provider of its GitHub account. No command creates a provider.",
	}
	provider.AddCommand(providerListCmd(a))

	rule := &cobra.Command{
		Use:   "rule",
		Short: "Manage workload trust rules",
		Long: "Manage workload trust rules. A rule gives one credential grant to the jobs of one provider whose " +
			"verified claims meet every condition of the rule. A rule stays live until you delete it.",
	}
	rule.AddCommand(ruleCreateCmd(a), ruleListCmd(a), ruleShowCmd(a), ruleDeleteCmd(a))

	exchange := &cobra.Command{
		Use:   "exchange",
		Short: "Inspect workload exchanges",
		Long: "Inspect the workload exchanges of the tenant: each time a CI job asked for a credential, and " +
			"whether SafeDep issued or denied it.",
	}
	exchange.AddCommand(exchangeListCmd(a))

	parent.AddCommand(provider, rule, exchange)
	root.AddCommand(parent)
}

// adminClient connects to the workload admin service with the control plane
// credentials of the active profile.
func adminClient(a *app.App) (controltowerv1grpc.WorkloadAdminServiceClient, error) {
	client, err := a.ControlPlane()
	if err != nil {
		return nil, err
	}
	return controltowerv1grpc.NewWorkloadAdminServiceClient(client.Connection()), nil
}
