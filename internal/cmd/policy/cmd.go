// Package policy registers the `safedep policy` command tree, the CLI for
// policy management in SafeDep Cloud: the policy settings document, policy
// changes, policy revisions, package rules and the decision log. Every
// verb needs control plane credentials.
package policy

import (
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "policy",
		Short: "Manage the policy of your SafeDep Cloud tenant",
		Long: "Manage the one policy of your SafeDep Cloud tenant. The policy settings document holds the " +
			"policy base and the policy overrides. Package rules trust or block packages. An edit of the " +
			"settings makes a policy change, which applies only after you publish it. Each publish makes " +
			"a policy revision.",
	}
	parent.AddCommand(showCmd(a), getCmd(a), editCmd(a))

	change := &cobra.Command{
		Use:   "change",
		Short: "Review, sync and publish policy changes",
		Long: "Review, edit, sync, publish and delete policy changes. A policy change is a draft on the " +
			"server. Nothing applies until you publish it. Create one with `safedep policy edit`, or with " +
			"`safedep policy change create --from-revision` for a rollback.",
	}
	change.AddCommand(
		changeListCmd(a),
		changeShowCmd(a),
		changeEditCmd(a),
		changeSyncCmd(a),
		changeDeleteCmd(a),
		changePublishCmd(a),
		changeCreateCmd(a),
	)

	revision := &cobra.Command{
		Use:   "revision",
		Short: "Inspect policy revisions",
		Long:  "Inspect policy revisions. Each publish makes one immutable revision. Revision numbers only go up.",
	}
	revision.AddCommand(revisionListCmd(a), revisionShowCmd(a))

	rule := &cobra.Command{
		Use:   "rule",
		Short: "Manage package rules: trust and block entries",
		Long: "Manage package rules. A block entry stops the install of the packages that it selects. A trust " +
			"entry waives verdicts of its trust scopes for one package, but never a verdict of verified " +
			"malware. Each create, update or delete publishes a new policy revision at once.",
	}
	rule.AddCommand(ruleCreateCmd(a), ruleListCmd(a), ruleShowCmd(a), ruleUpdateCmd(a), ruleDeleteCmd(a))

	decision := &cobra.Command{
		Use:   "decision",
		Short: "Read the decisions that the policy made",
		Long: "Read the decision log: the decisions that pmg and the pull request check made with the policy " +
			"of the tenant. Each decision cites the policy revision and the rule or field that decided, and " +
			"says where it happened. The roles that can read the policy can read the decision log.",
	}
	decision.AddCommand(decisionListCmd(a))

	parent.AddCommand(change, revision, rule, decision)
	root.AddCommand(parent)
}
