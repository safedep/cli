package policy

import (
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var fixedNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func failOnSpec(severity scangatev1.Severity) *specv1.PolicySpec {
	gate := &scangatev1.ScanGate{}
	gate.SetFailOn(severity)
	spec := &specv1.PolicySpec{}
	spec.SetScanGate(gate)
	return spec
}

func override(id, name string, priority uint32, labelKey, labelValue string, severity scangatev1.Severity) *policyv1.Override {
	sel := &messagescontroltowerv1.LabelSelector{}
	sel.SetMatchLabels(map[string]string{labelKey: labelValue})
	o := &policyv1.Override{}
	o.SetOverrideId(id)
	o.SetName(name)
	o.SetPriority(priority)
	o.SetSelector(sel)
	o.SetSpec(failOnSpec(severity))
	return o
}

func headPolicy() *policyv1.Policy {
	p := &policyv1.Policy{}
	p.SetBase(failOnSpec(scangatev1.Severity_SEVERITY_HIGH))
	p.SetOverrides([]*policyv1.Override{
		override("ov_pay", "payments", 200, "custom/criticality", "high", scangatev1.Severity_SEVERITY_HIGH),
		override("ov_ci", "ci", 300, "managed/deployment-kind", "ci", scangatev1.Severity_SEVERITY_CRITICAL),
	})
	return p
}

func actor(name string) *messagescontroltowerv1.Actor {
	a := &messagescontroltowerv1.Actor{}
	a.SetKind(messagescontroltowerv1.ActorKind_ACTOR_KIND_USER)
	a.SetId("u_" + name)
	a.SetDisplayName(name)
	return a
}

func revision(n uint64, message string) *policyv1.PolicyRevision {
	r := &policyv1.PolicyRevision{}
	r.SetRevision(n)
	r.SetAuthor(actor("alice"))
	r.SetMessage(message)
	r.SetSource(policyv1.ChangeSource_CHANGE_SOURCE_CLI)
	r.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_TIGHTENS)
	r.SetPublishedAt(timestamppb.New(time.Now().Add(-49 * time.Hour)))
	return r
}

func headResponse() *policyv1.GetPolicyResponse {
	def := &policyv1.DefaultPolicy{}
	def.SetRevision(12)
	def.SetSpec(failOnSpec(scangatev1.Severity_SEVERITY_HIGH))
	counts := &policyv1.PackageRuleCounts{}
	counts.SetTrustCount(184)
	counts.SetBlockCount(27)
	counts.SetTrustExpiringSoonCount(23)

	res := &policyv1.GetPolicyResponse{}
	res.SetPolicy(headPolicy())
	res.SetHeadRevision(46)
	res.SetHead(revision(46, "cooldown 5d for laptops"))
	res.SetDefaultPolicy(def)
	res.SetPackageRuleCounts(counts)
	return res
}

func openChange(id string) *policyv1.PolicyChange {
	c := &policyv1.PolicyChange{}
	c.SetChangeId(id)
	c.SetState(policyv1.PolicyChangeState_POLICY_CHANGE_STATE_OPEN)
	c.SetTitle("payments: medium + cooldown")
	c.SetBaseRevision(46)
	c.SetEtag("etag-1")
	c.SetAuthor(actor("alice"))
	c.SetUpdatedAt(timestamppb.New(fixedNow.Add(-2 * time.Minute)))
	return c
}

func reasonError(code codes.Code, reason errorv1.ErrorReason) error {
	detail := &errorv1.ErrorDetail{}
	detail.SetReason(reason)
	st, err := status.New(code, "rpc failed").WithDetails(detail)
	if err != nil {
		panic(err)
	}
	return st.Err()
}
