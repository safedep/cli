package cloudpolicy

import (
	"context"
	"fmt"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"google.golang.org/grpc"
)

// maxLabelSubjects is the limit of subjects in one label request.
const maxLabelSubjects = 100

// NewLabelClient returns the LabelService client of a control plane
// connection.
func NewLabelClient(conn grpc.ClientConnInterface) policyv1grpc.LabelServiceClient {
	return policyv1grpc.NewLabelServiceClient(conn)
}

// NewPolicyClient returns the PolicyService client of a control plane
// connection.
func NewPolicyClient(conn grpc.ClientConnInterface) policyv1grpc.PolicyServiceClient {
	return policyv1grpc.NewPolicyServiceClient(conn)
}

// SetLabels sets "custom/" labels on subjects of one kind.
func SetLabels(
	ctx context.Context,
	client policyv1grpc.LabelServiceClient,
	kind specv1.SubjectKind,
	ids []string,
	labels []*messagescontroltowerv1.Label,
) error {
	subjects, err := labelSubjects(kind, ids)
	if err != nil {
		return err
	}
	req := &policyv1.SetSubjectLabelsRequest{}
	req.SetSubjects(subjects)
	req.SetLabels(labels)
	if _, err := client.SetSubjectLabels(ctx, req); err != nil {
		return fmt.Errorf("set labels: %w", err)
	}
	return nil
}

// DeleteLabels removes "custom/" labels from subjects of one kind.
func DeleteLabels(
	ctx context.Context,
	client policyv1grpc.LabelServiceClient,
	kind specv1.SubjectKind,
	ids []string,
	keys []string,
) error {
	if err := ValidateCustomKeys(keys); err != nil {
		return err
	}
	subjects, err := labelSubjects(kind, ids)
	if err != nil {
		return err
	}
	req := &policyv1.DeleteSubjectLabelsRequest{}
	req.SetSubjects(subjects)
	req.SetKeys(keys)
	if _, err := client.DeleteSubjectLabels(ctx, req); err != nil {
		return fmt.Errorf("delete labels: %w", err)
	}
	return nil
}

func labelSubjects(kind specv1.SubjectKind, ids []string) ([]*specv1.Subject, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("give at least one %s", subjectKindName(kind))
	}
	if len(ids) > maxLabelSubjects {
		return nil, fmt.Errorf("give at most %d subjects in one command", maxLabelSubjects)
	}
	seen := make(map[string]struct{}, len(ids))
	subjects := make([]*specv1.Subject, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		subjects = append(subjects, NewSubject(kind, id))
	}
	return subjects, nil
}
