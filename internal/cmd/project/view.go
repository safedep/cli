package project

import (
	"time"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

type projectView struct {
	ID               string   `json:"project_id"`
	Name             string   `json:"name"`
	Source           string   `json:"source"`
	OriginURL        string   `json:"origin_url,omitempty"`
	Labels           []string `json:"labels"`
	Vulnerabilities  *uint32  `json:"vulnerabilities,omitempty"`
	PolicyViolations *uint32  `json:"policy_violations,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	created          *time.Time
}

func newProjectView(p *controltowerv1.ProjectWithAttributes) projectView {
	project := p.GetProject()
	attrs := p.GetAttributes()
	v := projectView{
		ID:        project.GetProjectId(),
		Name:      project.GetName(),
		Source:    tui.EnumToken(project.GetSource().String(), projectSourcePrefix),
		OriginURL: project.GetOriginUrl(),
		Labels:    cloudpolicy.FormatLabels(attrs.GetLabels()),
	}
	if attrs.HasVulnerabilities() {
		v.Vulnerabilities = new(attrs.GetVulnerabilities())
	}
	if attrs.HasPolicyViolations() {
		v.PolicyViolations = new(attrs.GetPolicyViolations())
	}
	if ts := project.GetCreatedAt(); ts != nil {
		t := ts.AsTime().UTC()
		v.created = &t
		v.CreatedAt = t.Format(time.RFC3339)
	}
	return v
}
