package cloudpolicy

import (
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	packageadmissionv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/packageadmission/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func specWith(failOn scangatev1.Severity, cooldown time.Duration) *specv1.PolicySpec {
	spec := &specv1.PolicySpec{}
	if failOn != scangatev1.Severity_SEVERITY_UNSPECIFIED {
		gate := &scangatev1.ScanGate{}
		gate.SetFailOn(failOn)
		spec.SetScanGate(gate)
	}
	if cooldown > 0 {
		c := &packageadmissionv1.Cooldown{}
		c.SetMinAge(durationpb.New(cooldown))
		pa := &packageadmissionv1.PackageAdmission{}
		pa.SetCooldown(c)
		spec.SetPackageAdmission(pa)
	}
	return spec
}

func samplePolicy() *policyv1.Policy {
	p := &policyv1.Policy{}
	p.SetBase(specWith(scangatev1.Severity_SEVERITY_HIGH, 48*time.Hour))

	sel := &messagescontroltowerv1.LabelSelector{}
	sel.SetMatchLabels(map[string]string{"custom/criticality": "high"})
	o := &policyv1.Override{}
	o.SetOverrideId("ov_1")
	o.SetName("payments")
	o.SetPriority(200)
	o.SetDescription("Stricter gate for payment services (CUST-412)")
	o.SetSelector(sel)
	o.SetSpec(specWith(scangatev1.Severity_SEVERITY_MEDIUM, 168*time.Hour))

	ci := &messagescontroltowerv1.LabelSelector{}
	ci.SetMatchLabels(map[string]string{"managed/deployment-kind": "ci"})
	o2 := &policyv1.Override{}
	o2.SetOverrideId("ov_2")
	o2.SetName("ci")
	o2.SetPriority(300)
	o2.SetSelector(ci)
	o2.SetSpec(specWith(scangatev1.Severity_SEVERITY_UNSPECIFIED, 168*time.Hour))

	p.SetOverrides([]*policyv1.Override{o2, o})
	return p
}
